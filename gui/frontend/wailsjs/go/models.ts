export namespace navi {
	
	export class MessageRecord {
	    id: string;
	    session_id: string;
	    role: string;
	    content: string;
	    // Go type: time
	    created_at: any;
	    prompt_tokens: number;
	    output_tokens: number;
	    cost: number;
	
	    static createFrom(source: any = {}) {
	        return new MessageRecord(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.session_id = source["session_id"];
	        this.role = source["role"];
	        this.content = source["content"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.prompt_tokens = source["prompt_tokens"];
	        this.output_tokens = source["output_tokens"];
	        this.cost = source["cost"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ResponseMeta {
	    cached: boolean;
	    prompt_tokens: number;
	    output_tokens: number;
	    turn_cost: number;
	    session_cost: number;
	    duration: number;
	
	    static createFrom(source: any = {}) {
	        return new ResponseMeta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.cached = source["cached"];
	        this.prompt_tokens = source["prompt_tokens"];
	        this.output_tokens = source["output_tokens"];
	        this.turn_cost = source["turn_cost"];
	        this.session_cost = source["session_cost"];
	        this.duration = source["duration"];
	    }
	}
	export class Result {
	    text: string;
	    meta: ResponseMeta;
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	        this.meta = this.convertValues(source["meta"], ResponseMeta);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ProviderInfo {
	    id: string;
	    name: string;
	    base_url: string;
	    is_local: boolean;
	    default_model: string;

	    static createFrom(source: any = {}) {
	        return new ProviderInfo(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.base_url = source["base_url"];
	        this.is_local = source["is_local"];
	        this.default_model = source["default_model"];
	    }
	}
	export class Session {
	    id: string;
	    title: string;
	    model: string;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	    messages?: MessageRecord[];
	
	    static createFrom(source: any = {}) {
	        return new Session(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.model = source["model"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	        this.messages = this.convertValues(source["messages"], MessageRecord);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Stats {
	    model: string;
	    input_tokens: number;
	    output_tokens: number;
	    total_cost: number;
	    cache_entries: number;
	    history_depth: number;
	
	    static createFrom(source: any = {}) {
	        return new Stats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.model = source["model"];
	        this.input_tokens = source["input_tokens"];
	        this.output_tokens = source["output_tokens"];
	        this.total_cost = source["total_cost"];
	        this.cache_entries = source["cache_entries"];
	        this.history_depth = source["history_depth"];
	    }
	}

}

