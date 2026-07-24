import XCTest
@testable import SpamFilter
import SpamFilterKit

@MainActor
final class LookupViewModelTests: XCTestCase {

    func test_lookup_success_setsLoadedPhaseWithData() async {
        let service = FakeLookup()
        service.result = NumberLookupData(
            number: "+14155550100",
            status: "blocked",
            action: "block",
            category: "scam",
            name: "Reported Scammer",
            spoofSuspected: true
        )
        let vm = LookupViewModel(service: service)
        vm.number = "+14155550100"

        await vm.lookup()

        XCTAssertEqual(vm.phase, .loaded(service.result))
        XCTAssertNil(vm.validationMessage)
    }

    func test_lookup_passesTrimmedNumber_toService() async {
        let service = FakeLookup()
        let vm = LookupViewModel(service: service)
        vm.number = "  +14155550123 "

        await vm.lookup()

        XCTAssertEqual(service.lookedUpNumbers, ["+14155550123"])
    }

    func test_lookup_invalidNumber_setsValidationMessage_andDoesNotCallService() async {
        let service = FakeLookup()
        let vm = LookupViewModel(service: service)
        vm.number = "abc"

        await vm.lookup()

        XCTAssertNotNil(vm.validationMessage)
        XCTAssertEqual(vm.phase, .idle)
        XCTAssertTrue(service.lookedUpNumbers.isEmpty)
    }

    func test_lookup_networkError_setsFailedPhaseWithMessage() async {
        let service = FakeLookup()
        service.error = APIClientError.api(code: "internal_error", message: "boom", field: nil, httpStatus: 500)
        let vm = LookupViewModel(service: service)
        vm.number = "+14155550100"

        await vm.lookup()

        XCTAssertEqual(vm.phase, .failed(message: "boom"))
    }

    func test_lookup_transitionsThroughLoading() async {
        let service = DelayingLookup()
        let vm = LookupViewModel(service: service)
        vm.number = "+14155550100"

        let task = Task { await vm.lookup() }
        await service.started.wait()
        XCTAssertEqual(vm.phase, .loading)
        service.resume()
        await task.value
        if case .loaded = vm.phase {} else { XCTFail("expected loaded, got \(vm.phase)") }
    }
}

private final class DelayingLookup: NumberLookup {
    let started = AsyncSignal()
    private let gate = AsyncSignal()

    func resume() { gate.signal() }

    func lookup(number: String) async throws -> NumberLookupData {
        started.signal()
        await gate.wait()
        return NumberLookupData(number: number, status: "blocked", action: "block", category: nil, name: nil, spoofSuspected: false)
    }
}
