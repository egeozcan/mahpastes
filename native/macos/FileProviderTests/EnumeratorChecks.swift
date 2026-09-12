import Foundation
import FileProvider

// Shared by XCTest and the command-line runner, so the native lifecycle tests
// can run on Macs with Command Line Tools but without a full Xcode install.
enum EnumeratorChecks {
    struct Failure: Error, CustomStringConvertible { let description: String }

    static func expiredAnchorCanRestart() async throws {
        let client = EnumerationFixture()
        let enumerator = ProviderEnumerator(client: client, scope: "active")
        let original = await currentAnchor(enumerator)
        try await page(enumerator, "")
        try await page(enumerator, "page-2")
        client.setHead("anchor-2")
        do {
            try await changes(enumerator, original!)
            throw Failure(description: "old anchor should expire")
        } catch let error as NSError where error.domain == NSFileProviderErrorDomain && error.code == NSFileProviderError.Code.syncAnchorExpired.rawValue {}
        let fresh = await currentAnchor(enumerator)
        guard fresh == Data("anchor-2".utf8) else {
            throw Failure(description: "re-enumeration reused the expired baseline")
        }
        try await page(enumerator, "")
        try await page(enumerator, "page-2")
        try await changes(enumerator, fresh!)
    }

    static func completedChangesReleaseBaseline() async throws {
        let client = EnumerationFixture()
        let enumerator = ProviderEnumerator(client: client, scope: "active")
        let baseline = await currentAnchor(enumerator)
        try await page(enumerator, "")
        try await page(enumerator, "page-2")
        try await changes(enumerator, baseline!)
        client.setHead("anchor-2")
        guard await currentAnchor(enumerator) == Data("anchor-2".utf8) else {
            throw Failure(description: "completed enumeration retained its old baseline")
        }
    }

    static func pageRetryRetainsBaselineUnlessExpired() async throws {
        for failure in [NSFileProviderError.Code.serverUnreachable, .pageExpired] {
            let client = EnumerationFixture()
            let enumerator = ProviderEnumerator(client: client, scope: "active")
            let baseline = await currentAnchor(enumerator)
            try await page(enumerator, "")
            client.setHead("anchor-2")
            client.failPage(with: failure)
            do {
                try await page(enumerator, "page-2")
                throw Failure(description: "expected failed continuation page")
            } catch let error as NSError where error.domain == NSFileProviderErrorDomain && error.code == failure.rawValue {}
            let after = await currentAnchor(enumerator)
            let expected = failure == .pageExpired ? Data("anchor-2".utf8) : baseline
            guard after == expected else {
                throw Failure(description: "incorrect baseline after continuation error \(failure)")
            }
            try await page(enumerator, failure == .pageExpired ? "" : "page-2")
        }
    }

    private static func currentAnchor(_ enumerator: ProviderEnumerator) async -> Data? {
        await withCheckedContinuation { continuation in
            enumerator.currentSyncAnchor { continuation.resume(returning: $0?.rawValue) }
        }
    }
    private static func page(_ enumerator: ProviderEnumerator, _ token: String) async throws {
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            let observer = PageObserver { continuation.resume(with: $0) }
            let page = token.isEmpty ? NSFileProviderPage(NSFileProviderPage.initialPageSortedByName as Data) : NSFileProviderPage(Data(token.utf8))
            enumerator.enumerateItems(for: observer, startingAt: page)
        }
    }
    private static func changes(_ enumerator: ProviderEnumerator, _ anchor: Data) async throws {
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            enumerator.enumerateChanges(for: ChangeObserver { continuation.resume(with: $0) }, from: NSFileProviderSyncAnchor(anchor))
        }
    }
}

private final class PageObserver: NSObject, NSFileProviderEnumerationObserver {
    let completion: (Result<Void, Error>) -> Void
    init(_ completion: @escaping (Result<Void, Error>) -> Void) { self.completion = completion }
    func didEnumerate(_ updatedItems: [NSFileProviderItem]) {}
    func finishEnumerating(upTo nextPage: NSFileProviderPage?) { completion(.success(())) }
    func finishEnumeratingWithError(_ error: Error) { completion(.failure(error)) }
}
private final class ChangeObserver: NSObject, NSFileProviderChangeObserver {
    let completion: (Result<Void, Error>) -> Void
    init(_ completion: @escaping (Result<Void, Error>) -> Void) { self.completion = completion }
    func didUpdate(_ updatedItems: [NSFileProviderItem]) {}
    func didDeleteItems(withIdentifiers deletedItemIdentifiers: [NSFileProviderItemIdentifier]) {}
    func finishEnumeratingChanges(upTo anchor: NSFileProviderSyncAnchor, moreComing: Bool) { completion(.success(())) }
    func finishEnumeratingWithError(_ error: Error) { completion(.failure(error)) }
}
private final class EnumerationFixture: ProviderEnumerationClient {
    private let lock = NSLock()
    private var head = "anchor-1"
    private var pageError: NSFileProviderError.Code?
    func setHead(_ value: String) { lock.lock(); defer { lock.unlock() }; head = value }
    func failPage(with error: NSFileProviderError.Code) { lock.lock(); defer { lock.unlock() }; pageError = error }
    func invalidate() {}
    private func response(_ route: String, _ query: [String: String]) throws -> Data {
        lock.lock(); defer { lock.unlock() }
        if route == "anchor" { return try JSONSerialization.data(withJSONObject: ["anchor": head]) }
        if route == "enumerate", let error = pageError { pageError = nil; throw NSFileProviderError(error) }
        if route == "changes", query["anchor"] != head { throw NSFileProviderError(.syncAnchorExpired) }
        return try JSONSerialization.data(withJSONObject: ["items": [], "deleted": [], "next": route == "enumerate" && query["page"] == "" ? "page-2" : "", "anchor": head, "more": false])
    }
    func get<T: Decodable>(_ route: String, _ query: [String: String]) async throws -> T {
        try JSONDecoder().decode(T.self, from: response(route, query))
    }
}
