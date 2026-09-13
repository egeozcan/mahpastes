import FileProvider
import UniformTypeIdentifiers

struct ItemRecord: Codable {
    let id: String
    let parent: String
    let name: String
    let mime: String
    let size: Int64
    let created: String
    let modified: String
    let contentVersion: String
    let metadataVersion: String
    let folder: Bool
}

struct ItemPage: Decodable {
    let items: [ItemRecord]
    let deleted: [String]
    let next: String
    let anchor: String
    let more: Bool
}

func providerID(_ id: String) -> NSFileProviderItemIdentifier {
    switch id {
    case "root": return .rootContainer
    case "working": return .workingSet
    default: return NSFileProviderItemIdentifier(id)
    }
}

func wireID(_ id: NSFileProviderItemIdentifier) -> String {
    switch id {
    case .rootContainer: return "root"
    case .workingSet: return "working"
    default: return id.rawValue
    }
}

final class ProviderItem: NSObject, NSFileProviderItem {
    let record: ItemRecord
    init(_ record: ItemRecord) { self.record = record }
    var itemIdentifier: NSFileProviderItemIdentifier { providerID(record.id) }
    var parentItemIdentifier: NSFileProviderItemIdentifier { providerID(record.parent) }
    var filename: String { record.name }
    var contentType: UTType {
        if record.folder { return .folder }
        return UTType(mimeType: record.mime.components(separatedBy: ";")[0])
            ?? UTType(filenameExtension: (record.name as NSString).pathExtension) ?? .data
    }
    var documentSize: NSNumber? { record.folder ? nil : NSNumber(value: record.size) }
    var creationDate: Date? { Self.date(record.created) }
    var contentModificationDate: Date? { Self.date(record.modified) }
    var itemVersion: NSFileProviderItemVersion {
        NSFileProviderItemVersion(contentVersion: Data(record.contentVersion.utf8),
                                  metadataVersion: Data(record.metadataVersion.utf8))
    }
    var capabilities: NSFileProviderItemCapabilities {
        record.folder ? [.allowsReading, .allowsContentEnumerating] : [.allowsReading]
    }
    private static func date(_ value: String) -> Date? {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter.date(from: value)
    }
}
