export namespace main {
	
	export class FalProcessingResult {
	    success: boolean;
	    clip_id?: number;
	    error?: string;
	    original_id: number;
	
	    static createFrom(source: any = {}) {
	        return new FalProcessingResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.clip_id = source["clip_id"];
	        this.error = source["error"];
	        this.original_id = source["original_id"];
	    }
	}
	export class FalTaskOptions {
	    task: string;
	    model?: string;
	    prompt?: string;
	    strength?: number;
	    fix_colors?: boolean;
	    remove_scratches?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FalTaskOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.task = source["task"];
	        this.model = source["model"];
	        this.prompt = source["prompt"];
	        this.strength = source["strength"];
	        this.fix_colors = source["fix_colors"];
	        this.remove_scratches = source["remove_scratches"];
	    }
	}
	export class AITask {
	    id: string;
	    task_name: string;
	    status: string;
	    clip_ids: number[];
	    options: FalTaskOptions;
	    progress: number;
	    total: number;
	    results?: FalProcessingResult[];
	    error?: string;
	    // Go type: time
	    created_at: any;
	
	    static createFrom(source: any = {}) {
	        return new AITask(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.task_name = source["task_name"];
	        this.status = source["status"];
	        this.clip_ids = source["clip_ids"];
	        this.options = this.convertValues(source["options"], FalTaskOptions);
	        this.progress = source["progress"];
	        this.total = source["total"];
	        this.results = this.convertValues(source["results"], FalProcessingResult);
	        this.error = source["error"];
	        this.created_at = this.convertValues(source["created_at"], null);
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
	export class ClipData {
	    id: number;
	    content_type: string;
	    data: string;
	    filename: string;
	
	    static createFrom(source: any = {}) {
	        return new ClipData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.content_type = source["content_type"];
	        this.data = source["data"];
	        this.filename = source["filename"];
	    }
	}
	export class Tag {
	    id: number;
	    name: string;
	    color: string;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new Tag(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.color = source["color"];
	        this.count = source["count"];
	    }
	}
	export class ClipPreview {
	    id: number;
	    content_type: string;
	    filename: string;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    expires_at?: any;
	    preview: string;
	    is_archived: boolean;
	    tags: Tag[];
	
	    static createFrom(source: any = {}) {
	        return new ClipPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.content_type = source["content_type"];
	        this.filename = source["filename"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.expires_at = this.convertValues(source["expires_at"], null);
	        this.preview = source["preview"];
	        this.is_archived = source["is_archived"];
	        this.tags = this.convertValues(source["tags"], Tag);
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
	
	
	export class FileData {
	    name: string;
	    content_type: string;
	    data: string;
	
	    static createFrom(source: any = {}) {
	        return new FileData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.content_type = source["content_type"];
	        this.data = source["data"];
	    }
	}
	
	export class WatchStatus {
	    global_paused: boolean;
	    active_count: number;
	    total_count: number;
	    is_watching: boolean;
	
	    static createFrom(source: any = {}) {
	        return new WatchStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.global_paused = source["global_paused"];
	        this.active_count = source["active_count"];
	        this.total_count = source["total_count"];
	        this.is_watching = source["is_watching"];
	    }
	}
	export class WatchedFolder {
	    id: number;
	    path: string;
	    filter_mode: string;
	    filter_presets: string[];
	    filter_regex: string;
	    process_existing: boolean;
	    auto_archive: boolean;
	    auto_tag_id?: number;
	    is_paused: boolean;
	    // Go type: time
	    created_at: any;
	    exists: boolean;
	
	    static createFrom(source: any = {}) {
	        return new WatchedFolder(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.path = source["path"];
	        this.filter_mode = source["filter_mode"];
	        this.filter_presets = source["filter_presets"];
	        this.filter_regex = source["filter_regex"];
	        this.process_existing = source["process_existing"];
	        this.auto_archive = source["auto_archive"];
	        this.auto_tag_id = source["auto_tag_id"];
	        this.is_paused = source["is_paused"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.exists = source["exists"];
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
	export class WatchedFolderConfig {
	    path: string;
	    filter_mode: string;
	    filter_presets: string[];
	    filter_regex: string;
	    process_existing: boolean;
	    auto_archive: boolean;
	    auto_tag_id?: number;
	
	    static createFrom(source: any = {}) {
	        return new WatchedFolderConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.filter_mode = source["filter_mode"];
	        this.filter_presets = source["filter_presets"];
	        this.filter_regex = source["filter_regex"];
	        this.process_existing = source["process_existing"];
	        this.auto_archive = source["auto_archive"];
	        this.auto_tag_id = source["auto_tag_id"];
	    }
	}

}

