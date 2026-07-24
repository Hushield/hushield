import Foundation

/// `data` payload for `POST /api/v1/attest/challenge`.
public struct ChallengeData: Decodable, Equatable {
    public let challenge: String
    public let expiresAt: String

    private enum CodingKeys: String, CodingKey {
        case challenge
        case expiresAt = "expires_at"
    }
}

/// `data` payload for `POST /api/v1/attest/verify` and `POST /api/v1/attest/assert`.
public struct DeviceTokenData: Decodable, Equatable {
    public let deviceToken: String
    public let expiresAt: String

    private enum CodingKeys: String, CodingKey {
        case deviceToken = "device_token"
        case expiresAt = "expires_at"
    }
}

/// `data` payload for `POST /api/v1/reports`.
public struct ReportResultData: Decodable, Equatable {
    public let number: String
    public let status: String

    public init(number: String, status: String) {
        self.number = number
        self.status = status
    }
}

/// One entry in the `/api/v1/blocklist` delta.
public struct BlocklistEntry: Decodable, Equatable {
    public let number: String
    public let action: String
    public let name: String?
    public let spoofSuspected: Bool

    private enum CodingKeys: String, CodingKey {
        case number, action, name
        case spoofSuspected = "spoof_suspected"
    }
}

/// `data` payload for `GET /api/v1/blocklist`.
public struct BlocklistData: Decodable, Equatable {
    public let entries: [BlocklistEntry]
    public let count: Int
    public let cursor: String
}

/// `data` payload for `GET /api/v1/numbers/{e164}`.
public struct NumberLookupData: Decodable, Equatable {
    public let number: String
    public let status: String
    public let action: String
    public let category: String?
    public let name: String?
    public let spoofSuspected: Bool

    private enum CodingKeys: String, CodingKey {
        case number, status, action, category, name
        case spoofSuspected = "spoof_suspected"
    }

    public init(number: String, status: String, action: String, category: String?, name: String?, spoofSuspected: Bool) {
        self.number = number
        self.status = status
        self.action = action
        self.category = category
        self.name = name
        self.spoofSuspected = spoofSuspected
    }
}
