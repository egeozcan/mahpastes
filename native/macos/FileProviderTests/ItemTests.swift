import XCTest
import FileProvider
import UniformTypeIdentifiers

final class ItemTests: XCTestCase {
    func testCancellationMappingIsIdempotent() {
        let cancelled = ProviderClient.map(CancellationError())
        let remapped = ProviderClient.map(cancelled) as NSError
        XCTAssertEqual(remapped.domain, NSCocoaErrorDomain)
        XCTAssertEqual(remapped.code, NSUserCancelledError)
        let unavailable = ProviderClient.map(NSFileProviderError(.versionNoLongerAvailable)) as NSError
        XCTAssertEqual(unavailable.domain, NSFileProviderErrorDomain)
        XCTAssertEqual(unavailable.code, NSFileProviderError.Code.versionNoLongerAvailable.rawValue)
    }

    func testReadOnlyCapabilitiesAndSeparateVersions() throws {
        let item = ProviderItem(ItemRecord(id: "epoch:uuid", parent: "active", name: "hello.txt",
            mime: "text/plain", size: 7, created: "2026-09-01T12:00:00.000Z",
            modified: "2026-09-02T12:00:00.000Z", contentVersion: "2", metadataVersion: "9", folder: false, hidden: false))
        XCTAssertEqual(item.capabilities, [.allowsReading])
        XCTAssertEqual(item.contentType, .plainText)
        XCTAssertEqual(item.documentSize, 7)
        XCTAssertEqual(item.itemVersion.contentVersion, Data("2".utf8))
        XCTAssertEqual(item.itemVersion.metadataVersion, Data("9".utf8))
        XCTAssertNotNil(item.creationDate)
        XCTAssertEqual(item.parentItemIdentifier.rawValue, "active")
    }

    func testFoldersDoNotAdvertiseImportOrDeletion() {
        let item = ProviderItem(ItemRecord(id: "active", parent: "root", name: "Active", mime: "inode/directory",
            size: 0, created: "", modified: "", contentVersion: "epoch", metadataVersion: "epoch", folder: true, hidden: false))
        XCTAssertEqual(item.capabilities, [.allowsReading, .allowsContentEnumerating])
        XCTAssertEqual(item.parentItemIdentifier, .rootContainer)
        XCTAssertEqual(item.contentType, .folder)
        XCTAssertNil(item.documentSize)
        XCTAssertEqual(wireID(providerID("working")), "working")
    }

    func testHiddenTagFolderRemainsReadable() throws {
        try ItemChecks.hiddenFoldersRemainReadable()
    }
}
