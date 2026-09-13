import Foundation
import FileProvider
import CryptoKit
import Security

private struct Discovery: Decodable {
    let `protocol`: Int
    let domain: String
    let endpoint: String
    let certificateSHA256: String
}

private final class PinnedSession: NSObject, URLSessionDelegate, URLSessionTaskDelegate {
    let fingerprint: String
    init(_ fingerprint: String) { self.fingerprint = fingerprint }
    func urlSession(_ session: URLSession, didReceive challenge: URLAuthenticationChallenge,
                    completionHandler: @escaping (URLSession.AuthChallengeDisposition, URLCredential?) -> Void) {
        guard challenge.protectionSpace.authenticationMethod == NSURLAuthenticationMethodServerTrust,
              challenge.protectionSpace.host == "127.0.0.1",
              let trust = challenge.protectionSpace.serverTrust,
              let certificates = SecTrustCopyCertificateChain(trust) as? [SecCertificate],
              let certificate = certificates.first else {
            completionHandler(.cancelAuthenticationChallenge, nil); return
        }
        let digest = SHA256.hash(data: SecCertificateCopyData(certificate) as Data)
            .map { String(format: "%02x", $0) }.joined()
        guard digest == fingerprint else { completionHandler(.cancelAuthenticationChallenge, nil); return }
        completionHandler(.useCredential, URLCredential(trust: trust))
    }
    func urlSession(_ session: URLSession, task: URLSessionTask,
                    willPerformHTTPRedirection response: HTTPURLResponse, newRequest request: URLRequest,
                    completionHandler: @escaping (URLRequest?) -> Void) {
        completionHandler(nil) // Never forward the domain credential to a redirect.
    }
}

final class ProviderClient {
    let domain: String
    private let lock = NSLock()
    private var sessions: [UUID: URLSession] = [:]
    private var invalidated = false

    init(domain: String) { self.domain = domain }

    func invalidate() {
        lock.lock(); invalidated = true; let active = Array(sessions.values); lock.unlock()
        active.forEach { $0.invalidateAndCancel() }
    }

    private func configuration() throws -> Discovery {
        guard domain.count == 32, domain.allSatisfy({ $0.isHexDigit }),
              let group = Bundle.main.object(forInfoDictionaryKey: "MahpastesAppGroup") as? String,
              let root = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: group) else {
            throw NSFileProviderError(.notAuthenticated)
        }
        do {
            let data = try Data(contentsOf: root.appendingPathComponent("provider-\(domain).json"))
            guard data.count < 4096 else { throw NSFileProviderError(.serverUnreachable) }
            let discovery = try JSONDecoder().decode(Discovery.self, from: data)
            guard discovery.protocol == 1, discovery.domain == domain,
                  discovery.certificateSHA256.count == 64,
                  let endpoint = URLComponents(string: discovery.endpoint),
                  endpoint.scheme == "https", endpoint.host == "127.0.0.1",
                  let port = endpoint.port, (1...65535).contains(port), endpoint.user == nil,
                  endpoint.password == nil, endpoint.query == nil, endpoint.fragment == nil,
                  endpoint.path.isEmpty else { throw NSFileProviderError(.serverUnreachable) }
            return discovery
        } catch { throw NSFileProviderError(.serverUnreachable) }
    }

    private func credential() throws -> String {
        guard let group = Bundle.main.object(forInfoDictionaryKey: "MahpastesKeychainGroup") as? String else {
            throw NSFileProviderError(.notAuthenticated)
        }
        let query: [CFString: Any] = [kSecClass: kSecClassGenericPassword, kSecUseDataProtectionKeychain: true,
            kSecAttrService: "MahpastesFileProvider", kSecAttrAccount: domain,
            kSecAttrAccessGroup: group, kSecReturnData: true]
        var result: CFTypeRef?
        guard SecItemCopyMatching(query as CFDictionary, &result) == errSecSuccess,
              let data = result as? Data else { throw NSFileProviderError(.notAuthenticated) }
        return data.base64EncodedString()
    }

    private func request(_ route: String, _ query: [String: String]) throws -> (URLRequest, URLSession, UUID) {
        let discovery = try configuration()
        guard var url = URLComponents(string: discovery.endpoint) else { throw NSFileProviderError(.serverUnreachable) }
        url.path = "/v1/" + route
        url.queryItems = (query.merging(["domain": domain]) { _, new in new })
            .map { URLQueryItem(name: $0.key, value: $0.value) }
        guard let address = url.url else { throw NSFileProviderError(.serverUnreachable) }
        var request = URLRequest(url: address)
        request.setValue("Bearer " + (try credential()), forHTTPHeaderField: "Authorization")
        let config = URLSessionConfiguration.ephemeral
        config.urlCache = nil; config.httpCookieStorage = nil; config.urlCredentialStorage = nil
        config.requestCachePolicy = .reloadIgnoringLocalCacheData
        config.timeoutIntervalForRequest = 30; config.timeoutIntervalForResource = 120
        config.connectionProxyDictionary = [:]
        let session = URLSession(configuration: config, delegate: PinnedSession(discovery.certificateSHA256), delegateQueue: nil)
        let id = UUID()
        lock.lock()
        if invalidated { lock.unlock(); session.invalidateAndCancel(); throw CancellationError() }
        sessions[id] = session; lock.unlock()
        return (request, session, id)
    }

    private func finish(_ id: UUID, _ session: URLSession) {
        lock.lock(); sessions.removeValue(forKey: id); lock.unlock()
        session.finishTasksAndInvalidate()
    }

    private func response(_ response: URLResponse, route: String) throws -> HTTPURLResponse {
        guard let response = response as? HTTPURLResponse else { throw NSFileProviderError(.serverUnreachable) }
        switch response.statusCode {
        case 200: return response
        case 401, 403: throw NSFileProviderError(.notAuthenticated)
        case 404: throw NSFileProviderError(.noSuchItem)
        case 409: throw NSFileProviderError(.versionNoLongerAvailable)
        case 410: throw NSFileProviderError(route == "enumerate" ? .pageExpired : .syncAnchorExpired)
        default: throw NSFileProviderError(.serverUnreachable)
        }
    }

    func get<T: Decodable>(_ route: String, _ query: [String: String]) async throws -> T {
        let (request, session, id) = try request(route, query); defer { finish(id, session) }
        do {
            let (data, raw) = try await session.data(for: request)
            _ = try response(raw, route: route)
            guard data.count <= 4 * 1024 * 1024 else { throw NSFileProviderError(.serverUnreachable) }
            return try JSONDecoder().decode(T.self, from: data)
        } catch { throw Self.map(error) }
    }

    func download(_ item: String, version: String?, directory: URL) async throws -> (URL, ItemRecord) {
        var query = ["id": item]; if let version = version { query["version"] = version }
        let (request, session, id) = try request("content", query); defer { finish(id, session) }
        let destination = directory.appendingPathComponent(UUID().uuidString)
        do {
            let (temporary, raw) = try await session.download(for: request)
            defer { try? FileManager.default.removeItem(at: temporary) }
            let response = try response(raw, route: "content")
            guard let header = response.value(forHTTPHeaderField: "X-Mahpastes-Item"),
                  let metadata = Data(base64Encoded: header) else { throw NSFileProviderError(.serverUnreachable) }
            let record = try JSONDecoder().decode(ItemRecord.self, from: metadata)
            let size = try FileManager.default.attributesOfItem(atPath: temporary.path)[.size] as? NSNumber
            guard record.id == item, !record.folder, record.size == size?.int64Value,
                  version == nil || record.contentVersion == version else { throw NSFileProviderError(.versionNoLongerAvailable) }
            try Task.checkCancellation()
            // URLSession's temporary file can be on a different volume. Copy
            // into the manager's staging directory before handing it to macOS.
            try FileManager.default.copyItem(at: temporary, to: destination)
            try Task.checkCancellation()
            return (destination, record)
        } catch {
            try? FileManager.default.removeItem(at: destination)
            throw Self.map(error)
        }
    }

    static func map(_ error: Error) -> Error {
        if (error as NSError).domain == NSCocoaErrorDomain && (error as NSError).code == NSUserCancelledError {
            return error
        }
        if error is CancellationError || (error as NSError).code == NSURLErrorCancelled {
            return NSError(domain: NSCocoaErrorDomain, code: NSUserCancelledError)
        }
        if (error as NSError).domain == NSFileProviderErrorDomain { return error }
        return NSFileProviderError(.serverUnreachable)
    }
}
