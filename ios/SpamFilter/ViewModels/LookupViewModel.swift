import Foundation
import Observation
import SpamFilterKit

/// Drives the Lookup screen: validates a number locally, then fetches its
/// community reputation via `NumberLookup`. The backend never 404s a lookup
/// -- an absent number comes back as a normal 200 with `status: "unknown"`,
/// which renders through the same `.loaded` phase.
@MainActor
@Observable
final class LookupViewModel {
    enum Phase: Equatable {
        case idle
        case loading
        case loaded(NumberLookupData)
        case failed(message: String)
    }

    var number: String = ""
    var validationMessage: String?
    var phase: Phase = .idle

    var isLoading: Bool { phase == .loading }

    private let service: NumberLookup

    init(service: NumberLookup) {
        self.service = service
    }

    func lookup() async {
        validationMessage = nil
        // The field holds display-formatted text, so normalize to E.164 first.
        guard let trimmed = PhoneFormatter.normalize(number) else {
            validationMessage = "Enter a valid phone number, e.g. (415) 555-1234."
            phase = .idle
            return
        }

        phase = .loading
        do {
            let data = try await service.lookup(number: trimmed)
            phase = .loaded(data)
        } catch {
            phase = .failed(message: ServiceErrorText.message(for: error))
        }
    }
}
