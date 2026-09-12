import XCTest

final class EnumeratorTests: XCTestCase {
    func testExpiredAnchorCanRestart() async throws { try await EnumeratorChecks.expiredAnchorCanRestart() }
    func testCompletedChangesReleaseBaseline() async throws { try await EnumeratorChecks.completedChangesReleaseBaseline() }
    func testPageRetryRetainsBaselineUnlessExpired() async throws { try await EnumeratorChecks.pageRetryRetainsBaselineUnlessExpired() }
}
