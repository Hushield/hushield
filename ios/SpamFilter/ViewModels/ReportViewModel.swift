import Foundation
import Observation
import SpamFilterKit

/// Drives the Report screen: collects a number + vote + category + optional
/// caller name, validates the number locally, and submits it via `Reporting`.
@MainActor
@Observable
final class ReportViewModel {
    enum Vote: String, CaseIterable, Identifiable {
        case spam
        case notSpam

        var wire: String { self == .spam ? "spam" : "not_spam" }
        var label: String { self == .spam ? "Spam" : "Not spam" }
        var symbol: String { self == .spam ? "exclamationmark.octagon.fill" : "checkmark.shield.fill" }
        var id: String { rawValue }
    }

    enum Category: String, CaseIterable, Identifiable {
        case scam
        case robocall
        case telemarketer
        case other

        var wire: String { rawValue }
        var label: String {
            switch self {
            case .scam: return "Scam"
            case .robocall: return "Robocall"
            case .telemarketer: return "Telemarketer"
            case .other: return "Other"
            }
        }
        var id: String { rawValue }
    }

    enum Phase: Equatable {
        case idle
        case submitting
        case success(status: String)
        case failed(message: String)
    }

    var number: String = ""
    var callerName: String = ""
    var vote: Vote = .spam
    var category: Category = .scam
    var validationMessage: String?
    var phase: Phase = .idle

    var isSubmitting: Bool { phase == .submitting }

    private let reporter: Reporting

    init(reporter: Reporting) {
        self.reporter = reporter
    }

    func submit() async {
        validationMessage = nil
        let trimmed = number.trimmingCharacters(in: .whitespacesAndNewlines)
        guard PhoneNumber.e164ToInt64(trimmed) != nil else {
            validationMessage = "Enter a valid number in E.164 format, e.g. +14155551234."
            phase = .idle
            return
        }

        phase = .submitting
        let trimmedName = callerName.trimmingCharacters(in: .whitespacesAndNewlines)
        do {
            let result = try await reporter.report(
                number: trimmed,
                vote: vote.wire,
                category: category.wire,
                name: trimmedName.isEmpty ? nil : trimmedName
            )
            phase = .success(status: result.status)
        } catch {
            phase = .failed(message: ServiceErrorText.message(for: error))
        }
    }
}
