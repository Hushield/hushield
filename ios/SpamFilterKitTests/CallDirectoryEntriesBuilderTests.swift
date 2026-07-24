import XCTest
@testable import SpamFilterKit

final class CallDirectoryEntriesBuilderTests: XCTestCase {

    // MARK: - Empty state -> empty arrays

    func test_emptyState_producesEmptyArrays() {
        let result = CallDirectoryEntriesBuilder.build(BlocklistState())

        XCTAssertTrue(result.blocking.isEmpty)
        XCTAssertTrue(result.identification.isEmpty)
    }

    // MARK: - blocking is state.blocked sorted ascending (CallKit invariant)

    func test_blocking_isSortedAscending() {
        let state = BlocklistState(
            blocked: [14_155_559_999, 12_125_550_000, 14_155_551_111],
            labeled: [],
            names: [:],
            cursor: "c1"
        )

        let result = CallDirectoryEntriesBuilder.build(state)

        XCTAssertEqual(result.blocking, [12_125_550_000, 14_155_551_111, 14_155_559_999])
        XCTAssertEqual(result.blocking, result.blocking.sorted(), "CallKit requires strictly ascending order")
    }

    // MARK: - identification is state.labeled sorted ascending, with labels

    func test_identification_isSortedAscending_withLabelsFromNames() {
        let state = BlocklistState(
            blocked: [],
            labeled: [14_155_559_999, 12_125_550_000],
            names: [14_155_559_999: "Robocaller Inc", 12_125_550_000: "Telemarketer"],
            cursor: "c1"
        )

        let result = CallDirectoryEntriesBuilder.build(state)

        XCTAssertEqual(result.identification.map { $0.number }, [12_125_550_000, 14_155_559_999])
        XCTAssertEqual(result.identification.map { $0.number }, result.identification.map { $0.number }.sorted())
        XCTAssertEqual(result.identification[0].label, "Telemarketer")
        XCTAssertEqual(result.identification[1].label, "Robocaller Inc")
    }

    // MARK: - identification label falls back to "Suspected Spam" when no name is known

    func test_identification_labelFallsBackToSuspectedSpam_whenNameMissing() {
        let state = BlocklistState(blocked: [], labeled: [14_155_552_222], names: [:], cursor: "c1")

        let result = CallDirectoryEntriesBuilder.build(state)

        XCTAssertEqual(result.identification.count, 1)
        XCTAssertEqual(result.identification[0].number, 14_155_552_222)
        XCTAssertEqual(result.identification[0].label, "Suspected Spam")
    }

    // MARK: - blocked and labeled sets are independent -- names on blocked entries never leak into identification

    func test_blockedEntries_neverAppearInIdentification() {
        let state = BlocklistState(
            blocked: [14_155_551_111],
            labeled: [14_155_552_222],
            names: [14_155_551_111: "Blocked Guy", 14_155_552_222: "Labeled Guy"],
            cursor: "c1"
        )

        let result = CallDirectoryEntriesBuilder.build(state)

        XCTAssertEqual(result.blocking, [14_155_551_111])
        XCTAssertEqual(result.identification.map { $0.number }, [14_155_552_222])
    }

    // MARK: - Large, unordered input still comes out strictly ascending (the mandatory CallKit invariant)

    func test_largeUnorderedInput_stillStrictlyAscending() {
        let numbers: [Int64] = (0..<200).map { _ in Int64.random(in: 1_000_000_000...19_999_999_999) }
        let state = BlocklistState(blocked: Set(numbers), labeled: Set(numbers.map { $0 + 20_000_000_000 }), names: [:], cursor: "c1")

        let result = CallDirectoryEntriesBuilder.build(state)

        XCTAssertEqual(result.blocking, result.blocking.sorted())
        XCTAssertEqual(result.identification.map { $0.number }, result.identification.map { $0.number }.sorted())
        // strictly ascending: no adjacent duplicates (Set already guarantees uniqueness, but assert anyway)
        for i in 1..<result.blocking.count {
            XCTAssertLessThan(result.blocking[i - 1], result.blocking[i])
        }
        for i in 1..<result.identification.count {
            XCTAssertLessThan(result.identification[i - 1].number, result.identification[i].number)
        }
    }
}
