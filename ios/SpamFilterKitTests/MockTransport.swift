import Foundation
@testable import SpamFilterKit

/// Test-only `HTTPTransport` that returns canned `(Data, HTTPURLResponse)`
/// pairs in FIFO order and records every request it receives, so tests can
/// assert on method, URL, headers, and body.
final class MockTransport: HTTPTransport {
    private(set) var recordedRequests: [URLRequest] = []
    private var queuedResponses: [(Data, HTTPURLResponse)] = []

    func enqueue(_ response: (Data, HTTPURLResponse)) {
        queuedResponses.append(response)
    }

    func send(_ request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        recordedRequests.append(request)
        guard !queuedResponses.isEmpty else {
            fatalError("MockTransport: no queued response for request \(request)")
        }
        return queuedResponses.removeFirst()
    }
}

func makeHTTPResponse(url: URL, status: Int) -> HTTPURLResponse {
    HTTPURLResponse(url: url, statusCode: status, httpVersion: "HTTP/1.1", headerFields: nil)!
}

func jsonData(_ string: String) -> Data {
    Data(string.utf8)
}
