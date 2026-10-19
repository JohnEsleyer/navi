export namespace navi {
	
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

