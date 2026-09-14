import FileProvider
import Foundation

// Shared by XCTest and the command-line runner so Finder item metadata can be
// checked on Macs that have Command Line Tools but not the full Xcode app.
enum ItemChecks {
    struct Failure: Error, CustomStringConvertible {
        let description: String
    }

    static func hiddenFoldersRemainReadable() throws {
        let item = ProviderItem(ItemRecord(
            id: "tag:epoch:7", parent: "tags", name: "private",
            mime: "inode/directory", size: 0, created: "", modified: "",
            contentVersion: "epoch", metadataVersion: "v", folder: true, hidden: true
        ))
        let expected: NSFileProviderFileSystemFlags = [.userReadable, .userExecutable, .hidden]
        guard item.fileSystemFlags == expected else {
            throw Failure(description: "hidden folder flags = \(item.fileSystemFlags), want \(expected)")
        }
    }
}
