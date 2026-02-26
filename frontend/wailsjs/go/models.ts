export namespace main {
	
	export class BackupSummary {
	    clips: number;
	    tags: number;
	    plugins: number;
	    watch_folders: number;
	
	    static createFrom(source: any = {}) {
	        return new BackupSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.clips = source["clips"];
	        this.tags = source["tags"];
	        this.plugins = source["plugins"];
	        this.watch_folders = source["watch_folders"];
	    }
	}
	export class BackupManifest {
	    format_version: number;
	    app_version: string;
	    // Go type: time
	    created_at: any;
	    platform: string;
	    summary: BackupSummary;
	    excluded: string[];
	
	    static createFrom(source: any = {}) {
	        return new BackupManifest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.format_version = source["format_version"];
	        this.app_version = source["app_version"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.platform = source["platform"];
	        this.summary = this.convertValues(source["summary"], BackupSummary);
	        this.excluded = source["excluded"];
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
	    size: number;
	    duplicate_count: number;
	
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
	        this.size = source["size"];
	        this.duplicate_count = source["duplicate_count"];
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
	export class DiffResult {
	    similarity: number;
	    diff_data_url: string;
	
	    static createFrom(source: any = {}) {
	        return new DiffResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.similarity = source["similarity"];
	        this.diff_data_url = source["diff_data_url"];
	    }
	}
	export class DragCapability {
	    enabled: boolean;
	    strategy: string;
	    reason: string;
	
	    static createFrom(source: any = {}) {
	        return new DragCapability(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.strategy = source["strategy"];
	        this.reason = source["reason"];
	    }
	}
	export class DuplicateGroup {
	    content_hash: string;
	    filename: string;
	    content_type: string;
	    count: number;
	    oldest_id: number;
	
	    static createFrom(source: any = {}) {
	        return new DuplicateGroup(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.content_hash = source["content_hash"];
	        this.filename = source["filename"];
	        this.content_type = source["content_type"];
	        this.count = source["count"];
	        this.oldest_id = source["oldest_id"];
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
	export class PluginInfo {
	    id: number;
	    name: string;
	    version: string;
	    description: string;
	    author: string;
	    enabled: boolean;
	    status: string;
	    events: string[];
	    settings: plugin.SettingField[];
	
	    static createFrom(source: any = {}) {
	        return new PluginInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.version = source["version"];
	        this.description = source["description"];
	        this.author = source["author"];
	        this.enabled = source["enabled"];
	        this.status = source["status"];
	        this.events = source["events"];
	        this.settings = this.convertValues(source["settings"], plugin.SettingField);
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
	export class PluginUIAction {
	    plugin_id: number;
	    plugin_name: string;
	    id: string;
	    label: string;
	    icon?: string;
	    async?: boolean;
	    options?: plugin.FormField[];
	    file_types?: string[];
	    max_size?: number;
	
	    static createFrom(source: any = {}) {
	        return new PluginUIAction(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.plugin_id = source["plugin_id"];
	        this.plugin_name = source["plugin_name"];
	        this.id = source["id"];
	        this.label = source["label"];
	        this.icon = source["icon"];
	        this.async = source["async"];
	        this.options = this.convertValues(source["options"], plugin.FormField);
	        this.file_types = source["file_types"];
	        this.max_size = source["max_size"];
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
	export class PrepareTransferRequest {
	    clip_id: number;
	    channel: string;
	
	    static createFrom(source: any = {}) {
	        return new PrepareTransferRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.clip_id = source["clip_id"];
	        this.channel = source["channel"];
	    }
	}
	export class PreparedTransferItem {
	    clip_id: number;
	    abs_path: string;
	    file_url: string;
	    filename: string;
	    content_type: string;
	    // Go type: time
	    lease_expires_at: any;
	
	    static createFrom(source: any = {}) {
	        return new PreparedTransferItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.clip_id = source["clip_id"];
	        this.abs_path = source["abs_path"];
	        this.file_url = source["file_url"];
	        this.filename = source["filename"];
	        this.content_type = source["content_type"];
	        this.lease_expires_at = this.convertValues(source["lease_expires_at"], null);
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
	export class StartNativeDragRequest {
	    clip_id: number;
	    abs_path: string;
	
	    static createFrom(source: any = {}) {
	        return new StartNativeDragRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.clip_id = source["clip_id"];
	        this.abs_path = source["abs_path"];
	    }
	}
	
	export class TransferCapabilities {
	    drag_out: DragCapability;
	    clipboard_file: boolean;
	
	    static createFrom(source: any = {}) {
	        return new TransferCapabilities(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.drag_out = this.convertValues(source["drag_out"], DragCapability);
	        this.clipboard_file = source["clipboard_file"];
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
	export class UIActionsResponse {
	    lightbox_buttons: PluginUIAction[];
	    card_actions: PluginUIAction[];
	
	    static createFrom(source: any = {}) {
	        return new UIActionsResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.lightbox_buttons = this.convertValues(source["lightbox_buttons"], PluginUIAction);
	        this.card_actions = this.convertValues(source["card_actions"], PluginUIAction);
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
	export class UpdateResult {
	    success: boolean;
	    needs_review: boolean;
	    preview?: plugin.PluginPreview;
	    plugin_info?: PluginInfo;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.needs_review = source["needs_review"];
	        this.preview = this.convertValues(source["preview"], plugin.PluginPreview);
	        this.plugin_info = this.convertValues(source["plugin_info"], PluginInfo);
	        this.error = source["error"];
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

export namespace plugin {
	
	export class ModalData {
	    plugin_id: number;
	    title: string;
	    content: string;
	    format: string;
	    copy_data?: string;
	    paste_data?: string;
	    paste_data_base64?: boolean;
	    paste_name?: string;
	    paste_content_type?: string;
	
	    static createFrom(source: any = {}) {
	        return new ModalData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.plugin_id = source["plugin_id"];
	        this.title = source["title"];
	        this.content = source["content"];
	        this.format = source["format"];
	        this.copy_data = source["copy_data"];
	        this.paste_data = source["paste_data"];
	        this.paste_data_base64 = source["paste_data_base64"];
	        this.paste_name = source["paste_name"];
	        this.paste_content_type = source["paste_content_type"];
	    }
	}
	export class ActionResult {
	    success: boolean;
	    error?: string;
	    result_clip_id?: number;
	    modal?: ModalData;
	
	    static createFrom(source: any = {}) {
	        return new ActionResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.error = source["error"];
	        this.result_clip_id = source["result_clip_id"];
	        this.modal = this.convertValues(source["modal"], ModalData);
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
	export class Choice {
	    value: string;
	    label: string;
	
	    static createFrom(source: any = {}) {
	        return new Choice(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.value = source["value"];
	        this.label = source["label"];
	    }
	}
	export class FilesystemPerms {
	    Read: boolean;
	    Write: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FilesystemPerms(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Read = source["Read"];
	        this.Write = source["Write"];
	    }
	}
	export class FormField {
	    id: string;
	    type: string;
	    label: string;
	    required?: boolean;
	    default?: any;
	    choices?: Choice[];
	    min?: number;
	    max?: number;
	    step?: number;
	
	    static createFrom(source: any = {}) {
	        return new FormField(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.type = source["type"];
	        this.label = source["label"];
	        this.required = source["required"];
	        this.default = source["default"];
	        this.choices = this.convertValues(source["choices"], Choice);
	        this.min = source["min"];
	        this.max = source["max"];
	        this.step = source["step"];
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
	
	export class PluginPreview {
	    name: string;
	    version: string;
	    description: string;
	    author: string;
	    network: Record<string, Array<string>>;
	    filesystem: FilesystemPerms;
	    clipboard: boolean;
	    events: string[];
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new PluginPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.version = source["version"];
	        this.description = source["description"];
	        this.author = source["author"];
	        this.network = source["network"];
	        this.filesystem = this.convertValues(source["filesystem"], FilesystemPerms);
	        this.clipboard = source["clipboard"];
	        this.events = source["events"];
	        this.source = source["source"];
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
	export class PluginUpdateInfo {
	    plugin_id: number;
	    current_version: string;
	    new_version: string;
	    has_permission_changes: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PluginUpdateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.plugin_id = source["plugin_id"];
	        this.current_version = source["current_version"];
	        this.new_version = source["new_version"];
	        this.has_permission_changes = source["has_permission_changes"];
	    }
	}
	export class SettingField {
	    key: string;
	    type: string;
	    label: string;
	    description?: string;
	    default?: any;
	    options?: string[];
	
	    static createFrom(source: any = {}) {
	        return new SettingField(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.type = source["type"];
	        this.label = source["label"];
	        this.description = source["description"];
	        this.default = source["default"];
	        this.options = source["options"];
	    }
	}

}

