import FileProvider

@objc(MahpastesFileProvider)
final class MahpastesFileProvider: NSObject, NSFileProviderReplicatedExtension {
    let client: ProviderClient
    let domain: NSFileProviderDomain

    required init(domain: NSFileProviderDomain) {
        self.domain = domain
        client = ProviderClient(domain: domain.identifier.rawValue)
        super.init()
    }

    func invalidate() { client.invalidate() }

    func enumerator(for containerItemIdentifier: NSFileProviderItemIdentifier,
                    request: NSFileProviderRequest) throws -> NSFileProviderEnumerator {
        let scope = wireID(containerItemIdentifier)
        guard ["root", "active", "archive", "working"].contains(scope) else { throw NSFileProviderError(.noSuchItem) }
        return ProviderEnumerator(domain: domain.identifier.rawValue, scope: scope)
    }

    func item(for identifier: NSFileProviderItemIdentifier, request: NSFileProviderRequest,
              completionHandler: @escaping (NSFileProviderItem?, Error?) -> Void) -> Progress {
        let progress = Progress(totalUnitCount: 1)
        let task = Task {
            do {
                let record: ItemRecord = try await client.get("item", ["id": wireID(identifier)])
                try Task.checkCancellation()
                progress.completedUnitCount = 1; completionHandler(ProviderItem(record), nil)
            } catch { completionHandler(nil, ProviderClient.map(error)) }
        }
        progress.cancellationHandler = { task.cancel() }
        return progress
    }

    func fetchContents(for identifier: NSFileProviderItemIdentifier, version: NSFileProviderItemVersion?,
                       request: NSFileProviderRequest,
                       completionHandler: @escaping (URL?, NSFileProviderItem?, Error?) -> Void) -> Progress {
        let progress = Progress(totalUnitCount: 1)
        let task = Task {
            do {
                guard let manager = NSFileProviderManager(for: domain) else { throw NSFileProviderError(.serverUnreachable) }
                var requested: String?
                if let version = version {
                    guard let value = String(data: version.contentVersion, encoding: .utf8), !value.isEmpty else {
                        throw NSFileProviderError(.versionNoLongerAvailable)
                    }
                    requested = value
                }
                let (url, record) = try await client.download(wireID(identifier), version: requested,
                                                            directory: manager.temporaryDirectoryURL())
                if Task.isCancelled {
                    try? FileManager.default.removeItem(at: url)
                    throw CancellationError()
                }
                // Successful handoff transfers staging-file ownership to macOS.
                progress.completedUnitCount = 1; completionHandler(url, ProviderItem(record), nil)
            } catch { completionHandler(nil, nil, ProviderClient.map(error)) }
        }
        progress.cancellationHandler = { task.cancel() }
        return progress
    }

    private var readOnly: Error { NSError(domain: NSCocoaErrorDomain, code: NSFileWriteNoPermissionError) }
    func createItem(basedOn itemTemplate: NSFileProviderItem, fields: NSFileProviderItemFields,
                    contents: URL?, options: NSFileProviderCreateItemOptions, request: NSFileProviderRequest,
                    completionHandler: @escaping (NSFileProviderItem?, NSFileProviderItemFields, Bool, Error?) -> Void) -> Progress {
        completionHandler(nil, fields, false, readOnly); return Progress(totalUnitCount: 0)
    }
    func modifyItem(_ item: NSFileProviderItem, baseVersion: NSFileProviderItemVersion,
                    changedFields: NSFileProviderItemFields, contents: URL?, options: NSFileProviderModifyItemOptions,
                    request: NSFileProviderRequest,
                    completionHandler: @escaping (NSFileProviderItem?, NSFileProviderItemFields, Bool, Error?) -> Void) -> Progress {
        completionHandler(nil, changedFields, false, readOnly); return Progress(totalUnitCount: 0)
    }
    func deleteItem(identifier: NSFileProviderItemIdentifier, baseVersion: NSFileProviderItemVersion,
                    options: NSFileProviderDeleteItemOptions, request: NSFileProviderRequest,
                    completionHandler: @escaping (Error?) -> Void) -> Progress {
        completionHandler(readOnly); return Progress(totalUnitCount: 0)
    }
}
