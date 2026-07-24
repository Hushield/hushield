import Foundation
import Observation
import SpamFilterKit

/// Drives the Lookup screen: validates a number locally, then fetches its
/// community reputation via `NumberLookup`. A 404 is treated as "no reports
/// yet" rather than an error.
@MainActor
@Observable
final class LookupViewModel {
    enum Phase: Equatable {
        case idle
        case loading
        case loaded(NumberLookupData)
        case notFound(number: String)
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
        let trimmed = number.trimmingCharacters(in: .whitespacesAndNewlines)
        guard PhoneNumber.e164ToInt64(trimmed) != nil else {
            validationMessage = "Enter a valid number in E.164 format, e.g. +14155551234."
            phase = .idle
            return
        }

        phase = .loading
        do {
            let data = try await service.lookup(number: trimmed)
            phase = .loaded(data)
        } catch let APIClientError.api(_, _, _, httpStatus) where httpStatus == 404 {
            phase = .notFound(number: trimmed)
        } catch {
            phase = .failed(message: ServiceErrorText.message(for: error))
        }
    }
}
