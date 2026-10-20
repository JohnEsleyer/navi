package navi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// StreamCallbacks delivers streamed turns to the UI in real time. Every hook
// is optional; AskStream only calls the ones that were supplied.
type StreamCallbacks struct {
	OnToken        func(token string)        // every generated token, as it arrives
	OnActionStart  func(tag, command string) // a closing action tag was intercepted
	OnActionResult func(tag, output string)  // the harness executed the action
	OnComplete     func(res *Result)         // the whole loop finished
	OnError        func(err error)           // any failure mid-loop
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	// Usage is present on the final chunk only when the server supports
	// stream_options.include_usage (OpenAI, Groq, OpenRouter, recent Ollama).
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// streamClient is separate from e.client because the engine's client carries
// a 60s total timeout for one-shot calls; a streamed agentic answer can run
// for many turns and must not be guillotined mid-generation.
var streamClient = &http.Client{Timeout: 10 * time.Minute}

// maxActionTurns bounds the interception loop. Without a ceiling a model that
// keeps emitting action tags in a cycle would loop forever; eight sequential
// actions comfortably covers the example workflow and its kin.
const maxActionTurns = 8

// AskStream runs the agentic streaming loop. The interception contract is
// strict and turn-based: the moment an enabled action's closing tag (e.g.
// `</terminal>`) completes in the live token stream, that turn is cut, the
// tag body is executed once, the output is appended to the context, and the
// next turn looks only at what the model generates afterwards.
func (e *Engine) AskStream(ctx context.Context, input string, cb StreamCallbacks) (*Result, error) {
	start := time.Now()

	e.historyMu.Lock()
	prospective := append([]Message{}, e.history...)
	prospective = append(prospective, Message{Role: "user", Content: input})
	e.historyMu.Unlock()

	// currentHistory is the working copy for the loop; shared history is only
	// rewritten (via commitExchange) once the complete exchange is known.
	currentHistory := prospective

	model := e.model()
	baseURL := e.baseURL()

	var fullText strings.Builder

	// lastUsage carries the final turn's reported token usage for billing.
	var lastUsage chatUsage

	for turn := 0; turn < maxActionTurns; turn++ {
		// The context window already carries the action catalogue in its
		// system message, so the request is built straight from the history
		// - no per-turn patching needed.
		turnMessages := make([]Message, len(currentHistory))
		copy(turnMessages, currentHistory)

		turnOutput, usage, intercepted, tagBody, err := e.streamSingleTurn(ctx, turnMessages, cb)
		if err != nil {
			if cb.OnError != nil {
				cb.OnError(err)
			}
			return nil, err
		}
		fullText.WriteString(turnOutput)
		lastUsage = usage

		// No action closed during this turn: the model is done answering.
		if intercepted == nil {
			break
		}

		// Strict live interception: execute exactly this command once.
		if cb.OnActionStart != nil {
			cb.OnActionStart(intercepted.Tag, strings.TrimSpace(tagBody))
		}
		actionResult := e.actions.ExecuteAction(ctx, *intercepted, tagBody)
		if cb.OnActionResult != nil {
			cb.OnActionResult(intercepted.Tag, actionResult)
		}

		// Feed the partial assistant turn plus the harness output back in.
		// The system role is used for the harness turn because every provider
		// in the registry treats it as trusted instruction content.
		currentHistory = append(currentHistory,
			Message{Role: "assistant", Content: turnOutput},
			Message{Role: "system", Content: actionResult},
		)
	}

	finalText := fullText.String()
	e.commitExchange(input, finalText)

	// Cost accounting uses the final-chunk usage when the provider reported
	// it; streamed turns without usage data report zero tokens rather than
	// inventing an estimate.
	inT, outT := lastUsage.PromptTokens, lastUsage.CompletionTokens
	turnCost := computeTurnCost(DetectProvider(model), baseURL, model, inT, outT)
	sessionCost := e.addUsage(inT, outT, turnCost)
	e.persistExchange(input, finalText, model, inT, outT, turnCost)

	res := &Result{
		Text: finalText,
		Meta: ResponseMeta{
			Cached:       false,
			PromptTokens: inT,
			OutputTokens: outT,
			TurnCost:     turnCost,
			SessionCost:  sessionCost,
			Duration:     time.Since(start),
		},
	}

	if cb.OnComplete != nil {
		cb.OnComplete(res)
	}
	return res, nil
}

// streamSingleTurn streams one model turn and watches the token stream for a
// completed action closing tag. It stops reading as soon as a tag closes -
// the rest of that generation is abandoned, which is exactly what "pause"
// looks like on a one-way HTTP stream. The usage struct is filled only if
// the server sent it on the final chunk of this turn.
func (e *Engine) streamSingleTurn(
	ctx context.Context,
	messages []Message,
	cb StreamCallbacks,
) (turnText string, usage chatUsage, intercepted *Action, tagBody string, err error) {
	bodyBytes, err := json.Marshal(chatStreamRequest{
		Model:       e.model(),
		Messages:    messages,
		Temperature: 0.2,
		Stream:      true,
	})
	if err != nil {
		return "", usage, nil, "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", e.baseURL()+"/chat/completions", bytes.NewBuffer(bodyBytes))
	if err != nil {
		return "", usage, nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if key := e.apiKey(); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	resp, err := streamClient.Do(req)
	if err != nil {
		return "", usage, nil, "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", usage, nil, "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}

	reader := bufio.NewReader(resp.Body)
	var turnAccumulator strings.Builder

	scan := func() (intercepted *Action, body string) {
		// Interception fires the instant a closing tag completes - checking
		// the accumulator after every token, not after the stream ends.
		if !e.actions.IsActionsEnabled() {
			return nil, ""
		}
		soFar := turnAccumulator.String()
		for _, act := range e.actions.ListActions() {
			if !act.Enabled {
				continue
			}
			closeTag := "</" + act.Tag + ">"
			openTag := "<" + act.Tag + ">"
			idxClose := strings.LastIndex(soFar, closeTag)
			if idxClose == -1 {
				continue
			}
			idxOpen := strings.LastIndex(soFar[:idxClose], "<"+act.Tag)
			if idxOpen == -1 || idxOpen >= idxClose {
				continue
			}
			// Require the real opening tag form "<tag>" (or "<tag ...") so a
			// similar prefix tag like <term> never matches </terminal>.
			openSnippet := soFar[idxOpen:]
			if !strings.HasPrefix(openSnippet, openTag) && !strings.HasPrefix(openSnippet, openTag[:len(openTag)-1]+" ") {
				continue
			}
			return &act, soFar[idxOpen+len(openTag) : idxClose]
		}
		return nil, ""
	}

	for {
		line, readErr := reader.ReadString('\n')
		if line != "" {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "data: ") {
				data := strings.TrimPrefix(trimmed, "data: ")
				if data == "[DONE]" {
					break
				}
				var chunk streamChunk
				if err := json.Unmarshal([]byte(data), &chunk); err == nil {
					if chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 {
						usage.PromptTokens = chunk.Usage.PromptTokens
						usage.CompletionTokens = chunk.Usage.CompletionTokens
					}
					if len(chunk.Choices) > 0 {
						token := chunk.Choices[0].Delta.Content
						if token != "" {
							turnAccumulator.WriteString(token)
							if cb.OnToken != nil {
								cb.OnToken(token)
							}
							if act, body := scan(); act != nil {
								return turnAccumulator.String(), usage, act, strings.TrimSpace(body), nil
							}
						}
					}
				}
			}
		}
		if readErr != nil {
			break
		}
	}

	return turnAccumulator.String(), usage, nil, "", nil
}

// chatStreamRequest mirrors chatRequest with the streaming flag added.
type chatStreamRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float32   `json:"temperature"`
	Stream      bool      `json:"stream"`
}

// chatUsage carries the token usage reported on a stream's final chunk.
type chatUsage struct {
	PromptTokens     int
	CompletionTokens int
}
