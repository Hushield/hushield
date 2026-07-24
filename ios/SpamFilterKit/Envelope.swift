import Foundation

/// A single error entry in an envelope's `errors` array.
public struct APIError: Decodable, Equatable {
    public let field: String
    public let message: String
    public let code: String
}

/// Metadata attached to every `/api/v1` response envelope.
public struct Meta: Decodable, Equatable {
    public let timestamp: String
    public let requestId: String

    private enum CodingKeys: String, CodingKey {
        case timestamp
        case requestId = "request_id"
    }
}

/// The exact JSON response shape returned by every `/api/v1` endpoint:
/// `{ success, data, errors, meta }`.
public struct Envelope<T: Decodable>: Decodable {
    public let success: Bool
    public let data: T?
    public let errors: [APIError]
    public let meta: Meta
}

/// Typed error thrown by `APIClient` for both API-reported failures and
/// local request/response handling failures.
public enum APIClientError: Error, Equatable {
    /// The server responded with `success: false`. Carries the first
    /// `APIError` entry's code/message/field, plus the HTTP status code.
    case api(code: String, message: String, field: String?, httpStatus: Int?)
    /// The response body could not be decoded as the expected envelope shape.
    case decodingFailed(description: String)
    /// The request could not be constructed (e.g. an invalid URL).
    case invalidRequest(description: String)
}
