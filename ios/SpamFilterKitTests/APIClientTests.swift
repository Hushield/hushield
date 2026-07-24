import XCTest
@testable import SpamFilterKit

final class APIClientTests: XCTestCase {
    private let baseURL = URL(string: "https://api.example.test")!

    private func makeClient(transport: MockTransport, tokenProvider: (() -> String?)? = nil) -> APIClient {
        APIClient(transport: transport, baseURL: baseURL, tokenProvider: tokenProvider)
    }

    private func bodyJSON(_ request: URLRequest) throws -> [String: Any] {
        let data = try XCTUnwrap(request.httpBody)
        let object = try JSONSerialization.jsonObject(with: data)
        return try XCTUnwrap(object as? [String: Any])
    }

    // MARK: - challenge()

    func test_challenge_sendsCorrectMethodAndPath_noAuthHeader() async throws {
        let transport = MockTransport()
        transport.enqueue((jsonData("""
        {"success":true,"data":{"challenge":"Y2hhbGxlbmdl","expires_at":"2026-07-23T12:05:00Z"},"errors":[],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"req-1"}}
        """), makeHTTPResponse(url: baseURL, status: 200)))
        let client = makeClient(transport: transport)

        _ = try await client.challenge()

        let request = try XCTUnwrap(transport.recordedRequests.first)
        XCTAssertEqual(request.httpMethod, "POST")
        XCTAssertEqual(request.url?.path, "/api/v1/attest/challenge")
        XCTAssertNil(request.url?.query)
        XCTAssertNil(request.value(forHTTPHeaderField: "Authorization"))
        XCTAssertNil(request.httpBody)
    }

    func test_challenge_decodesSuccessResponse() async throws {
        let transport = MockTransport()
        transport.enqueue((jsonData("""
        {"success":true,"data":{"challenge":"Y2hhbGxlbmdl","expires_at":"2026-07-23T12:05:00Z"},"errors":[],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"req-1"}}
        """), makeHTTPResponse(url: baseURL, status: 200)))
        let client = makeClient(transport: transport)

        let result = try await client.challenge()

        XCTAssertEqual(result.challenge, "Y2hhbGxlbmdl")
        XCTAssertEqual(result.expiresAt, "2026-07-23T12:05:00Z")
    }

    // MARK: - verify()

    func test_verify_sendsCorrectMethodPathBody_noAuthHeader() async throws {
        let transport = MockTransport()
        transport.enqueue((jsonData("""
        {"success":true,"data":{"device_token":"dtok","expires_at":"2026-07-24T12:00:00Z"},"errors":[],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"req-2"}}
        """), makeHTTPResponse(url: baseURL, status: 200)))
        let client = makeClient(transport: transport)

        _ = try await client.verify(keyID: "key-1", attestationB64: "YXR0ZXN0", challengeB64: "Y2hhbA==")

        let request = try XCTUnwrap(transport.recordedRequests.first)
        XCTAssertEqual(request.httpMethod, "POST")
        XCTAssertEqual(request.url?.path, "/api/v1/attest/verify")
        XCTAssertNil(request.value(forHTTPHeaderField: "Authorization"))
        let body = try bodyJSON(request)
        XCTAssertEqual(body["key_id"] as? String, "key-1")
        XCTAssertEqual(body["attestation"] as? String, "YXR0ZXN0")
        XCTAssertEqual(body["challenge"] as? String, "Y2hhbA==")
        XCTAssertEqual(body.count, 3)
    }

    func test_verify_decodesSuccessResponse() async throws {
        let transport = MockTransport()
        transport.enqueue((jsonData("""
        {"success":true,"data":{"device_token":"dtok","expires_at":"2026-07-24T12:00:00Z"},"errors":[],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"req-2"}}
        """), makeHTTPResponse(url: baseURL, status: 200)))
        let client = makeClient(transport: transport)

        let result = try await client.verify(keyID: "key-1", attestationB64: "YXR0ZXN0", challengeB64: "Y2hhbA==")

        XCTAssertEqual(result.deviceToken, "dtok")
        XCTAssertEqual(result.expiresAt, "2026-07-24T12:00:00Z")
    }

    // MARK: - assert()

    func test_assert_sendsCorrectMethodPathBody_noAuthHeader() async throws {
        let transport = MockTransport()
        transport.enqueue((jsonData("""
        {"success":true,"data":{"device_token":"dtok2","expires_at":"2026-07-24T13:00:00Z"},"errors":[],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"req-3"}}
        """), makeHTTPResponse(url: baseURL, status: 200)))
        let client = makeClient(transport: transport)

        _ = try await client.assert(keyID: "key-2", assertionB64: "YXNzZXJ0", challengeB64: "Y2gy")

        let request = try XCTUnwrap(transport.recordedRequests.first)
        XCTAssertEqual(request.httpMethod, "POST")
        XCTAssertEqual(request.url?.path, "/api/v1/attest/assert")
        XCTAssertNil(request.value(forHTTPHeaderField: "Authorization"))
        let body = try bodyJSON(request)
        XCTAssertEqual(body["key_id"] as? String, "key-2")
        XCTAssertEqual(body["assertion"] as? String, "YXNzZXJ0")
        XCTAssertEqual(body["challenge"] as? String, "Y2gy")
        XCTAssertEqual(body.count, 3)
    }

    func test_assert_decodesSuccessResponse() async throws {
        let transport = MockTransport()
        transport.enqueue((jsonData("""
        {"success":true,"data":{"device_token":"dtok2","expires_at":"2026-07-24T13:00:00Z"},"errors":[],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"req-3"}}
        """), makeHTTPResponse(url: baseURL, status: 200)))
        let client = makeClient(transport: transport)

        let result = try await client.assert(keyID: "key-2", assertionB64: "YXNzZXJ0", challengeB64: "Y2gy")

        XCTAssertEqual(result.deviceToken, "dtok2")
        XCTAssertEqual(result.expiresAt, "2026-07-24T13:00:00Z")
    }

    // MARK: - report()

    func test_report_sendsAllFields_withAuthHeader() async throws {
        let transport = MockTransport()
        transport.enqueue((jsonData("""
        {"success":true,"data":{"number":"+14155550100","status":"suspected"},"errors":[],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"req-4"}}
        """), makeHTTPResponse(url: baseURL, status: 201)))
        let client = makeClient(transport: transport)

        _ = try await client.report(number: "4155550100", vote: "spam", category: "scam", name: "Bad Actor", token: "dev-token")

        let request = try XCTUnwrap(transport.recordedRequests.first)
        XCTAssertEqual(request.httpMethod, "POST")
        XCTAssertEqual(request.url?.path, "/api/v1/reports")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer dev-token")
        let body = try bodyJSON(request)
        XCTAssertEqual(body["number"] as? String, "4155550100")
        XCTAssertEqual(body["vote"] as? String, "spam")
        XCTAssertEqual(body["category"] as? String, "scam")
        XCTAssertEqual(body["name"] as? String, "Bad Actor")
        XCTAssertEqual(body.count, 4)
    }

    func test_report_omitsOptionalFieldsWhenNil() async throws {
        let transport = MockTransport()
        transport.enqueue((jsonData("""
        {"success":true,"data":{"number":"+14155550100","status":"unknown"},"errors":[],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"req-5"}}
        """), makeHTTPResponse(url: baseURL, status: 201)))
        let client = makeClient(transport: transport)

        _ = try await client.report(number: "4155550100", token: "dev-token")

        let request = try XCTUnwrap(transport.recordedRequests.first)
        let body = try bodyJSON(request)
        XCTAssertEqual(body["number"] as? String, "4155550100")
        XCTAssertNil(body["vote"])
        XCTAssertNil(body["category"])
        XCTAssertNil(body["name"])
        XCTAssertEqual(body.count, 1)
    }

    func test_report_decodesSuccessResponse() async throws {
        let transport = MockTransport()
        transport.enqueue((jsonData("""
        {"success":true,"data":{"number":"+14155550100","status":"blocked"},"errors":[],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"req-6"}}
        """), makeHTTPResponse(url: baseURL, status: 201)))
        let client = makeClient(transport: transport)

        let result = try await client.report(number: "4155550100", token: "dev-token")

        XCTAssertEqual(result.number, "+14155550100")
        XCTAssertEqual(result.status, "blocked")
    }

    func test_report_noTokenNoProvider_omitsAuthHeader() async throws {
        let transport = MockTransport()
        transport.enqueue((jsonData("""
        {"success":true,"data":{"number":"+14155550100","status":"unknown"},"errors":[],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"req-7"}}
        """), makeHTTPResponse(url: baseURL, status: 201)))
        let client = makeClient(transport: transport)

        _ = try await client.report(number: "4155550100")

        let request = try XCTUnwrap(transport.recordedRequests.first)
        XCTAssertNil(request.value(forHTTPHeaderField: "Authorization"))
    }

    // MARK: - blocklist()

    func test_blocklist_sendsAllQueryParams_withAuthHeader() async throws {
        let transport = MockTransport()
        transport.enqueue((jsonData("""
        {"success":true,"data":{"entries":[],"count":0,"cursor":"0.0"},"errors":[],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"req-8"}}
        """), makeHTTPResponse(url: baseURL, status: 200)))
        let client = makeClient(transport: transport)

        _ = try await client.blocklist(since: "1700000000.42", prefix: "415555", limit: 250, token: "dev-token")

        let request = try XCTUnwrap(transport.recordedRequests.first)
        XCTAssertEqual(request.httpMethod, "GET")
        XCTAssertEqual(request.url?.path, "/api/v1/blocklist")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer dev-token")
        let components = try XCTUnwrap(URLComponents(url: XCTUnwrap(request.url), resolvingAgainstBaseURL: false))
        let items = try XCTUnwrap(components.queryItems)
        XCTAssertEqual(Set(items.map { "\($0.name)=\($0.value ?? "")" }), Set([
            "since=1700000000.42", "prefix=415555", "limit=250",
        ]))
    }

    func test_blocklist_omitsQueryParamsWhenNil() async throws {
        let transport = MockTransport()
        transport.enqueue((jsonData("""
        {"success":true,"data":{"entries":[],"count":0,"cursor":"0.0"},"errors":[],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"req-9"}}
        """), makeHTTPResponse(url: baseURL, status: 200)))
        let client = makeClient(transport: transport)

        _ = try await client.blocklist(token: "dev-token")

        let request = try XCTUnwrap(transport.recordedRequests.first)
        XCTAssertNil(request.url?.query)
    }

    func test_blocklist_decodesEntries_includingNullNameAndEmptyList() async throws {
        let transport = MockTransport()
        transport.enqueue((jsonData("""
        {"success":true,"data":{"entries":[{"number":"+14155550100","action":"block","name":null,"spoof_suspected":false},{"number":"+14155550101","action":"label","name":"Some Co","spoof_suspected":true}],"count":2,"cursor":"1700000000.5"},"errors":[],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"req-10"}}
        """), makeHTTPResponse(url: baseURL, status: 200)))
        let client = makeClient(transport: transport)

        let result = try await client.blocklist(token: "dev-token")

        XCTAssertEqual(result.count, 2)
        XCTAssertEqual(result.cursor, "1700000000.5")
        XCTAssertEqual(result.entries.count, 2)
        XCTAssertEqual(result.entries[0].number, "+14155550100")
        XCTAssertEqual(result.entries[0].action, "block")
        XCTAssertNil(result.entries[0].name)
        XCTAssertEqual(result.entries[0].spoofSuspected, false)
        XCTAssertEqual(result.entries[1].name, "Some Co")
        XCTAssertEqual(result.entries[1].spoofSuspected, true)
    }

    func test_blocklist_decodesEmptyEntries() async throws {
        let transport = MockTransport()
        transport.enqueue((jsonData("""
        {"success":true,"data":{"entries":[],"count":0,"cursor":"0.0"},"errors":[],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"req-11"}}
        """), makeHTTPResponse(url: baseURL, status: 200)))
        let client = makeClient(transport: transport)

        let result = try await client.blocklist(token: "dev-token")

        XCTAssertEqual(result.entries, [])
        XCTAssertEqual(result.count, 0)
        XCTAssertEqual(result.cursor, "0.0")
    }

    // MARK: - lookup()

    func test_lookup_pathEncodesLiteralPlus() async throws {
        let transport = MockTransport()
        transport.enqueue((jsonData("""
        {"success":true,"data":{"number":"+14155550100","status":"unknown","action":"none","category":null,"name":null,"spoof_suspected":false},"errors":[],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"req-12"}}
        """), makeHTTPResponse(url: baseURL, status: 200)))
        let client = makeClient(transport: transport)

        _ = try await client.lookup(number: "+14155550100", token: "dev-token")

        let request = try XCTUnwrap(transport.recordedRequests.first)
        let components = try XCTUnwrap(URLComponents(url: XCTUnwrap(request.url), resolvingAgainstBaseURL: false))
        XCTAssertEqual(components.percentEncodedPath, "/api/v1/numbers/%2B14155550100")
    }

    func test_lookup_pathWithoutPlus() async throws {
        let transport = MockTransport()
        transport.enqueue((jsonData("""
        {"success":true,"data":{"number":"+14155550100","status":"unknown","action":"none","category":null,"name":null,"spoof_suspected":false},"errors":[],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"req-13"}}
        """), makeHTTPResponse(url: baseURL, status: 200)))
        let client = makeClient(transport: transport)

        _ = try await client.lookup(number: "14155550100", token: "dev-token")

        let request = try XCTUnwrap(transport.recordedRequests.first)
        let components = try XCTUnwrap(URLComponents(url: XCTUnwrap(request.url), resolvingAgainstBaseURL: false))
        XCTAssertEqual(components.percentEncodedPath, "/api/v1/numbers/14155550100")
    }

    func test_lookup_omitsPrefixQueryWhenNil() async throws {
        let transport = MockTransport()
        transport.enqueue((jsonData("""
        {"success":true,"data":{"number":"+14155550100","status":"unknown","action":"none","category":null,"name":null,"spoof_suspected":false},"errors":[],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"req-14"}}
        """), makeHTTPResponse(url: baseURL, status: 200)))
        let client = makeClient(transport: transport)

        _ = try await client.lookup(number: "+14155550100", token: "dev-token")

        let request = try XCTUnwrap(transport.recordedRequests.first)
        XCTAssertNil(request.url?.query)
    }

    func test_lookup_sendsPrefixQuery_withAuthHeader() async throws {
        let transport = MockTransport()
        transport.enqueue((jsonData("""
        {"success":true,"data":{"number":"+14155550100","status":"unknown","action":"none","category":null,"name":null,"spoof_suspected":true},"errors":[],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"req-15"}}
        """), makeHTTPResponse(url: baseURL, status: 200)))
        let client = makeClient(transport: transport)

        _ = try await client.lookup(number: "+14155550100", prefix: "415555", token: "dev-token")

        let request = try XCTUnwrap(transport.recordedRequests.first)
        XCTAssertEqual(request.httpMethod, "GET")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer dev-token")
        let components = try XCTUnwrap(URLComponents(url: XCTUnwrap(request.url), resolvingAgainstBaseURL: false))
        XCTAssertEqual(components.queryItems, [URLQueryItem(name: "prefix", value: "415555")])
    }

    func test_lookup_decodesSuccessResponse_includingNullCategoryAndName() async throws {
        let transport = MockTransport()
        transport.enqueue((jsonData("""
        {"success":true,"data":{"number":"+14155550100","status":"unknown","action":"none","category":null,"name":null,"spoof_suspected":true},"errors":[],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"req-16"}}
        """), makeHTTPResponse(url: baseURL, status: 200)))
        let client = makeClient(transport: transport)

        let result = try await client.lookup(number: "+14155550100", token: "dev-token")

        XCTAssertEqual(result.number, "+14155550100")
        XCTAssertEqual(result.status, "unknown")
        XCTAssertEqual(result.action, "none")
        XCTAssertNil(result.category)
        XCTAssertNil(result.name)
        XCTAssertEqual(result.spoofSuspected, true)
    }

    func test_lookup_decodesSuccessResponse_withCategoryAndName() async throws {
        let transport = MockTransport()
        transport.enqueue((jsonData("""
        {"success":true,"data":{"number":"+14155550100","status":"blocked","action":"block","category":"scam","name":"Bad Actor","spoof_suspected":false},"errors":[],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"req-17"}}
        """), makeHTTPResponse(url: baseURL, status: 200)))
        let client = makeClient(transport: transport)

        let result = try await client.lookup(number: "+14155550100", token: "dev-token")

        XCTAssertEqual(result.category, "scam")
        XCTAssertEqual(result.name, "Bad Actor")
    }

    // MARK: - Token provider fallback

    func test_tokenProvider_usedWhenPerCallTokenNil() async throws {
        let transport = MockTransport()
        transport.enqueue((jsonData("""
        {"success":true,"data":{"entries":[],"count":0,"cursor":"0.0"},"errors":[],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"req-18"}}
        """), makeHTTPResponse(url: baseURL, status: 200)))
        let client = makeClient(transport: transport, tokenProvider: { "provider-token" })

        _ = try await client.blocklist()

        let request = try XCTUnwrap(transport.recordedRequests.first)
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer provider-token")
    }

    func test_perCallToken_overridesTokenProvider() async throws {
        let transport = MockTransport()
        transport.enqueue((jsonData("""
        {"success":true,"data":{"entries":[],"count":0,"cursor":"0.0"},"errors":[],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"req-19"}}
        """), makeHTTPResponse(url: baseURL, status: 200)))
        let client = makeClient(transport: transport, tokenProvider: { "provider-token" })

        _ = try await client.blocklist(token: "explicit-token")

        let request = try XCTUnwrap(transport.recordedRequests.first)
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer explicit-token")
    }

    // MARK: - Error envelopes

    func test_errorEnvelope_badRequest400_throwsTypedError() async throws {
        let transport = MockTransport()
        transport.enqueue((jsonData("""
        {"success":false,"data":null,"errors":[{"field":"key_id","message":"key_id is required","code":"bad_request"}],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"req-20"}}
        """), makeHTTPResponse(url: baseURL, status: 400)))
        let client = makeClient(transport: transport)

        do {
            _ = try await client.verify(keyID: "", attestationB64: "x", challengeB64: "y")
            XCTFail("expected error to be thrown")
        } catch APIClientError.api(let code, let message, let field, let httpStatus) {
            XCTAssertEqual(code, "bad_request")
            XCTAssertEqual(message, "key_id is required")
            XCTAssertEqual(field, "key_id")
            XCTAssertEqual(httpStatus, 400)
        }
    }

    func test_errorEnvelope_unauthorized401_throwsTypedError() async throws {
        let transport = MockTransport()
        transport.enqueue((jsonData("""
        {"success":false,"data":null,"errors":[{"field":"","message":"device authentication required","code":"unauthorized"}],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"req-21"}}
        """), makeHTTPResponse(url: baseURL, status: 401)))
        let client = makeClient(transport: transport)

        do {
            _ = try await client.blocklist(token: "bad-token")
            XCTFail("expected error to be thrown")
        } catch APIClientError.api(let code, let message, let field, let httpStatus) {
            XCTAssertEqual(code, "unauthorized")
            XCTAssertEqual(message, "device authentication required")
            XCTAssertNil(field)
            XCTAssertEqual(httpStatus, 401)
        }
    }

    func test_errorEnvelope_invalid422_withField_throwsTypedError() async throws {
        let transport = MockTransport()
        transport.enqueue((jsonData("""
        {"success":false,"data":null,"errors":[{"field":"number","message":"invalid phone number","code":"invalid"}],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"req-22"}}
        """), makeHTTPResponse(url: baseURL, status: 422)))
        let client = makeClient(transport: transport)

        do {
            _ = try await client.report(number: "abc", token: "dev-token")
            XCTFail("expected error to be thrown")
        } catch APIClientError.api(let code, let message, let field, let httpStatus) {
            XCTAssertEqual(code, "invalid")
            XCTAssertEqual(message, "invalid phone number")
            XCTAssertEqual(field, "number")
            XCTAssertEqual(httpStatus, 422)
        }
    }

    func test_errorEnvelope_internalError500_throwsTypedError() async throws {
        let transport = MockTransport()
        transport.enqueue((jsonData("""
        {"success":false,"data":null,"errors":[{"field":"","message":"failed to load blocklist","code":"internal_error"}],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"req-23"}}
        """), makeHTTPResponse(url: baseURL, status: 500)))
        let client = makeClient(transport: transport)

        do {
            _ = try await client.blocklist(token: "dev-token")
            XCTFail("expected error to be thrown")
        } catch APIClientError.api(let code, let message, let field, let httpStatus) {
            XCTAssertEqual(code, "internal_error")
            XCTAssertEqual(message, "failed to load blocklist")
            XCTAssertNil(field)
            XCTAssertEqual(httpStatus, 500)
        }
    }

    // MARK: - Malformed body

    func test_malformedBody_throwsDecodingError() async throws {
        let transport = MockTransport()
        transport.enqueue((jsonData("not valid json at all"), makeHTTPResponse(url: baseURL, status: 200)))
        let client = makeClient(transport: transport)

        do {
            _ = try await client.challenge()
            XCTFail("expected error to be thrown")
        } catch APIClientError.decodingFailed {
            // expected
        }
    }

    func test_malformedBody_wrongShape_throwsDecodingError() async throws {
        let transport = MockTransport()
        transport.enqueue((jsonData("""
        {"success":true,"data":{"unexpected":"shape"},"errors":[],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"req-24"}}
        """), makeHTTPResponse(url: baseURL, status: 200)))
        let client = makeClient(transport: transport)

        do {
            _ = try await client.challenge()
            XCTFail("expected error to be thrown")
        } catch APIClientError.decodingFailed {
            // expected
        }
    }
}
