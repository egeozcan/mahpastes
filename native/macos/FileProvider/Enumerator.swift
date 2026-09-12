import FileProvider

final class ProviderEnumerator: NSObject, NSFileProviderEnumerator {
    private let client: ProviderClient
    private let scope: String
    private let anchor: EnumerationAnchor

    init(domain: String, scope: String) {
        let client = ProviderClient(domain: domain)
        self.client = client; self.scope = scope
        self.anchor = EnumerationAnchor(client: client, scope: scope)
    }
    func invalidate() { client.invalidate() }

    func enumerateItems(for observer: NSFileProviderEnumerationObserver, startingAt page: NSFileProviderPage) {
        Task {
            do {
                let initial = page == .initialPageSortedByName || page == .initialPageSortedByDate
                let value = initial ? "" : String(data: page.rawValue, encoding: .utf8)
                guard let value = value else { throw NSFileProviderError(.pageExpired) }
                // Capture the baseline BEFORE creating the initial snapshot.
                // Concurrent currentSyncAnchor calls share this same task.
                _ = try await anchor.value()
                let result: ItemPage = try await client.get("enumerate", ["scope": scope, "page": value])
                observer.didEnumerate(result.items.map { ProviderItem($0) })
                observer.finishEnumerating(upTo: result.next.isEmpty ? nil : NSFileProviderPage(Data(result.next.utf8)))
            } catch { observer.finishEnumeratingWithError(ProviderClient.map(error)) }
        }
    }

    func enumerateChanges(for observer: NSFileProviderChangeObserver, from syncAnchor: NSFileProviderSyncAnchor) {
        Task {
            do {
                guard let anchor = String(data: syncAnchor.rawValue, encoding: .utf8) else { throw NSFileProviderError(.syncAnchorExpired) }
                let result: ItemPage = try await client.get("changes", ["scope": scope, "anchor": anchor])
                observer.didDeleteItems(withIdentifiers: result.deleted.map { providerID($0) })
                observer.didUpdate(result.items.map { ProviderItem($0) })
                observer.finishEnumeratingChanges(upTo: NSFileProviderSyncAnchor(Data(result.anchor.utf8)), moreComing: result.more)
            } catch { observer.finishEnumeratingWithError(ProviderClient.map(error)) }
        }
    }

    func currentSyncAnchor(completionHandler: @escaping (NSFileProviderSyncAnchor?) -> Void) {
        Task {
            do { completionHandler(NSFileProviderSyncAnchor(Data(try await anchor.value().utf8))) }
            catch { completionHandler(nil) }
        }
    }
}

private actor EnumerationAnchor {
    let client: ProviderClient
    let scope: String
    var pending: Task<String, Error>?
    init(client: ProviderClient, scope: String) { self.client = client; self.scope = scope }
    func value() async throws -> String {
        if let pending = pending { return try await pending.value }
        let task = Task<String, Error> {
            struct Response: Decodable { let anchor: String }
            let response: Response = try await client.get("anchor", ["scope": scope])
            return response.anchor
        }
        pending = task
        do { return try await task.value }
        catch { pending = nil; throw error }
    }
}
