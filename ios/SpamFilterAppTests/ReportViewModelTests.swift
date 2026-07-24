import XCTest
@testable import SpamFilter
import SpamFilterKit

@MainActor
final class ReportViewModelTests: XCTestCase {

    func test_submit_success_setsSuccessPhaseWithReturnedStatus() async {
        let reporter = FakeReporter()
        reporter.result = ReportResultData(number: "+14155550100", status: "blocked")
        let vm = ReportViewModel(reporter: reporter)
        vm.number = "+14155550100"

        await vm.submit()

        XCTAssertEqual(vm.phase, .success(status: "blocked"))
        XCTAssertNil(vm.validationMessage)
    }

    func test_submit_passesTrimmedNumberVoteCategoryAndName_toService() async {
        let reporter = FakeReporter()
        let vm = ReportViewModel(reporter: reporter)
        vm.number = "  +14155550123 "
        vm.vote = .notSpam
        vm.category = .robocall
        vm.callerName = "Acme Collections"

        await vm.submit()

        XCTAssertEqual(reporter.calls.count, 1)
        XCTAssertEqual(
            reporter.calls.first,
            FakeReporter.Call(number: "+14155550123", vote: "not_spam", category: "robocall", name: "Acme Collections")
        )
    }

    func test_submit_emptyName_passesNilName() async {
        let reporter = FakeReporter()
        let vm = ReportViewModel(reporter: reporter)
        vm.number = "+14155550123"
        vm.callerName = ""

        await vm.submit()

        XCTAssertEqual(reporter.calls.first?.name, nil)
    }

    func test_submit_invalidNumber_setsValidationMessage_andDoesNotCallService() async {
        let reporter = FakeReporter()
        let vm = ReportViewModel(reporter: reporter)
        vm.number = "not-a-number"

        await vm.submit()

        XCTAssertNotNil(vm.validationMessage)
        XCTAssertEqual(vm.phase, .idle)
        XCTAssertTrue(reporter.calls.isEmpty)
    }

    func test_submit_networkError_setsFailedPhaseWithMessage() async {
        let reporter = FakeReporter()
        reporter.error = APIClientError.api(code: "rate_limited", message: "Too many reports", field: nil, httpStatus: 429)
        let vm = ReportViewModel(reporter: reporter)
        vm.number = "+14155550100"

        await vm.submit()

        XCTAssertEqual(vm.phase, .failed(message: "Too many reports"))
    }

    func test_submit_transitionsThroughSubmitting() async {
        let reporter = DelayingReporter()
        let vm = ReportViewModel(reporter: reporter)
        vm.number = "+14155550100"

        let task = Task { await vm.submit() }
        await reporter.started.value        // service entered
        XCTAssertEqual(vm.phase, .submitting)
        reporter.resume()
        await task.value
        XCTAssertEqual(vm.phase, .success(status: "blocked"))
    }
}

/// Reporter that blocks inside `report` until `resume()` is called, so a test
/// can observe the `.submitting` phase mid-flight.
private final class DelayingReporter: Reporting {
    let started = AsyncSignal()
    private let gate = AsyncSignal()

    func resume() { gate.signal() }

    func report(number: String, vote: String?, category: String?, name: String?) async throws -> ReportResultData {
        started.signal()
        await gate.wait()
        return ReportResultData(number: number, status: "blocked")
    }
}

/// One-shot async signal usable across tasks.
final class AsyncSignal {
    private var continuations: [CheckedContinuation<Void, Never>] = []
    private var signaled = false
    private let lock = NSLock()

    var value: Void {
        get async { await wait() }
    }

    func signal() {
        lock.lock()
        signaled = true
        let waiters = continuations
        continuations.removeAll()
        lock.unlock()
        waiters.forEach { $0.resume() }
    }

    func wait() async {
        await withCheckedContinuation { continuation in
            lock.lock()
            if signaled {
                lock.unlock()
                continuation.resume()
            } else {
                continuations.append(continuation)
                lock.unlock()
            }
        }
    }
}
