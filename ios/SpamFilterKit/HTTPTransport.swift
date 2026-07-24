import Foundation

/// Abstraction over "send an HTTP request, get back raw data + response."
/// `APIClient` talks only to this protocol, so tests can inject a mock and
/// never touch the network.
public protocol HTTPTransport {
    func send(_ request: URLRequest) async throws -> (Data, HTTPURLResponse)
}

/// Thin `URLSession`-backed `HTTPTransport`. Not unit-tested directly here;
/// exercised by the Task 7 integration tests against a live/local backend.
public final class URLSessionTransport: HTTPTransport {
    private let session: URLSession

    public init(session: URLSession = .shared) {
        self.session = session
    }

    public func send(_ request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        let (data, response) = try await session.data(for: request)
        guard let httpResponse = response as? HTTPURLResponse else {
            throw APIClientError.invalidRequest(description: "response was not an HTTP response")
        }
        return (data, httpResponse)
    }
}
