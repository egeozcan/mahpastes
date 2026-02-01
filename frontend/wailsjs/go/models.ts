export namespace main {
	
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
	    }
	}

}

