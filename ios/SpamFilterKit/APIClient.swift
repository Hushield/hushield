import Foundation

/// Talks to the SpamFilter `/api/v1` backend over an injected `HTTPTransport`.
/// Every method builds a `URLRequest`, sends it through the transport, decodes
/// the response envelope, and either returns the `data` payload or throws an
/// `APIClientError`.
public final class APIClient {
    private let transport: HTTPTransport
    private let baseURL: URL
    private let tokenProvider: (() -> String?)?
    private let decoder: JSONDecoder
    private let encoder: JSONEncoder

    /// - Parameters:
    ///   - transport: the `HTTPTransport` to send requests through.
    ///   - baseURL: the API base URL (e.g. `http://localhost:8080`), with no
    ///     trailing `/api/v1` -- each method appends its own path.
    ///   - tokenProvider: optional fallback used to supply the device token
    ///     for authenticated endpoints when no per-call `token` is given.
    public init(transport: HTTPTransport, baseURL: URL, tokenProvider: (() -> String?)? = nil) {
        self.transport = transport
        self.baseURL = baseURL
        self.tokenProvider = tokenProvider
        self.decoder = JSONDecoder()
        self.encoder = JSONEncoder()
    }

    // MARK: - Attestation (unauthenticated)

    /// `POST /api/v1/attest/challenge`
    public func challenge() async throws -> ChallengeData {
        try await sendRequest(method: "POST", path: "/api/v1/attest/challenge", authenticated: false, token: nil)
    }

    /// `POST /api/v1/attest/verify`
    public func verify(keyID: String, attestationB64: String, challengeB64: String) async throws -> DeviceTokenData {
        let body = VerifyRequestBody(keyID: keyID, attestation: attestationB64, challenge: challengeB64)
        return try await sendRequest(method: "POST", path: "/api/v1/attest/verify", body: body, authenticated: false, token: nil)
    }

    /// `POST /api/v1/attest/assert`
    public func assert(keyID: String, assertionB64: String, challengeB64: String) async throws -> DeviceTokenData {
        let body = AssertRequestBody(keyID: keyID, assertion: assertionB64, challenge: challengeB64)
        return try await sendRequest(method: "POST", path: "/api/v1/attest/assert", body: body, authenticated: false, token: nil)
    }

    // MARK: - Device-authed endpoints

    /// `POST /api/v1/reports`
    public func report(
        number: String,
        vote: String? = nil,
        category: String? = nil,
        name: String? = nil,
        token: String? = nil
    ) async throws -> ReportResultData {
        let body = ReportRequestBody(number: number, category: category, vote: vote, name: name)
        return try await sendRequest(method: "POST", path: "/api/v1/reports", body: body, authenticated: true, token: token)
    }

    /// `GET /api/v1/blocklist`
    public func blocklist(
        since: String? = nil,
        prefix: String? = nil,
        limit: Int? = nil,
        token: String? = nil
    ) async throws -> BlocklistData {
        var query: [URLQueryItem] = []
        if let since { query.append(URLQueryItem(name: "since", value: since)) }
        if let prefix { query.append(URLQueryItem(name: "prefix", value: prefix)) }
        if let limit { query.append(URLQueryItem(name: "limit", value: String(limit))) }
        return try await sendRequest(method: "GET", path: "/api/v1/blocklist", query: query, authenticated: true, token: token)
    }

    /// `GET /api/v1/numbers/{e164}`
    public func lookup(number: String, prefix: String? = nil, token: String? = nil) async throws -> NumberLookupData {
        guard let encodedNumber = number.addingPercentEncoding(withAllowedCharacters: Self.pathAllowedCharacterSet) else {
            throw APIClientError.invalidRequest(description: "unable to percent-encode number path segment")
        }
        var query: [URLQueryItem] = []
        if let prefix { query.append(URLQueryItem(name: "prefix", value: prefix)) }
        return try await sendRequest(
            method: "GET",
            path: "/api/v1/numbers/\(encodedNumber)",
            query: query,
            authenticated: true,
            token: token
        )
    }

    // MARK: - Request building

    /// Path-segment allowed characters: standard URL path allowed set, minus
    /// `+`, so a literal `+` in an E.164 number is always percent-encoded to
    /// `%2B` rather than passed through unescaped.
    private static let pathAllowedCharacterSet: CharacterSet = {
        var allowed = CharacterSet.urlPathAllowed
        allowed.remove(charactersIn: "+")
        return allowed
    }()

    private func buildURL(path: String, query: [URLQueryItem]) throws -> URL {
        guard var components = URLComponents(url: baseURL, resolvingAgainstBaseURL: false) else {
            throw APIClientError.invalidRequest(description: "invalid base URL")
        }
        components.percentEncodedPath += path
        components.queryItems = query.isEmpty ? nil : query
        guard let url = components.url else {
            throw APIClientError.invalidRequest(description: "failed to construct request URL for path \(path)")
        }
        return url
    }

    private func sendRequest<T: Decodable>(
        method: String,
        path: String,
        query: [URLQueryItem] = [],
        authenticated: Bool,
        token: String?
    ) async throws -> T {
        try await sendRequest(method: method, path: path, query: query, bodyData: nil, authenticated: authenticated, token: token)
    }

    private func sendRequest<T: Decodable, B: Encodable>(
        method: String,
        path: String,
        body: B,
        authenticated: Bool,
        token: String?
    ) async throws -> T {
        let bodyData = try encoder.encode(body)
        return try await sendRequest(method: method, path: path, query: [], bodyData: bodyData, authenticated: authenticated, token: token)
    }

    private func sendRequest<T: Decodable>(
        method: String,
        path: String,
        query: [URLQueryItem],
        bodyData: Data?,
        authenticated: Bool,
        token: String?
    ) async throws -> T {
        let url = try buildURL(path: path, query: query)
        var request = URLRequest(url: url)
        request.httpMethod = method
        if let bodyData {
            request.httpBody = bodyData
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        if authenticated, let resolvedToken = token ?? tokenProvider?() {
            request.setValue("Bearer \(resolvedToken)", forHTTPHeaderField: "Authorization")
        }

        let (data, response) = try await transport.send(request)

        let envelope: Envelope<T>
        do {
            envelope = try decoder.decode(Envelope<T>.self, from: data)
        } catch {
            throw APIClientError.decodingFailed(description: String(describing: error))
        }

        guard envelope.success, let payload = envelope.data else {
            let firstError = envelope.errors.first
            let field = (firstError?.field.isEmpty == false) ? firstError?.field : nil
            throw APIClientError.api(
                code: firstError?.code ?? "unknown",
                message: firstError?.message ?? "unknown error",
                field: field,
                httpStatus: response.statusCode
            )
        }
        return payload
    }
}

// MARK: - Request bodies

/// Encodes optional fields with `encodeIfPresent` so nil values are omitted
/// from the JSON body entirely, rather than encoded as `null`.
private struct VerifyRequestBody: Encodable {
    let keyID: String
    let attestation: String
    let challenge: String

    private enum CodingKeys: String, CodingKey {
        case keyID = "key_id"
        case attestation
        case challenge
    }
}

private struct AssertRequestBody: Encodable {
    let keyID: String
    let assertion: String
    let challenge: String

    private enum CodingKeys: String, CodingKey {
        case keyID = "key_id"
        case assertion
        case challenge
    }
}

private struct ReportRequestBody: Encodable {
    let number: String
    let category: String?
    let vote: String?
    let name: String?

    private enum CodingKeys: String, CodingKey {
        case number, category, vote, name
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(number, forKey: .number)
        try container.encodeIfPresent(category, forKey: .category)
        try container.encodeIfPresent(vote, forKey: .vote)
        try container.encodeIfPresent(name, forKey: .name)
    }
}
