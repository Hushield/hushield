import XCTest
@testable import SpamFilterKit

final class BlocklistStateTests: XCTestCase {

    private func entry(_ number: String, _ action: String, name: String? = nil, spoofSuspected: Bool = false) -> BlocklistEntry {
        BlocklistEntry(number: number, action: action, name: name, spoofSuspected: spoofSuspected)
    }

    // MARK: - Fresh snapshot

    func test_freshSnapshot_appliesBlockLabelAndNames() {
        let state = BlocklistState()
        let entries = [
            entry("+14155551111", "block", name: "Robocaller Inc"),
            entry("+14155552222", "label", name: "Telemarketer"),
            entry("+14155553333", "block"),
        ]

        let next = state.applying(entries, newCursor: "1690000000.100")

        XCTAssertEqual(next.blocked, [14_155_551_111, 14_155_553_333])
        XCTAssertEqual(next.labeled, [14_155_552_222])
        XCTAssertEqual(next.names, [14_155_551_111: "Robocaller Inc", 14_155_552_222: "Telemarketer"])
        XCTAssertEqual(next.cursor, "1690000000.100")
    }

    // MARK: - Disjointness invariant

    func test_blockedAndLabeled_areAlwaysDisjoint() {
        var state = BlocklistState()
        let n = "+14155551111"

        state = state.applying([entry(n, "block")], newCursor: "c1")
        XCTAssertTrue(state.blocked.contains(14_155_551_111))
        XCTAssertFalse(state.labeled.contains(14_155_551_111))

        state = state.applying([entry(n, "label")], newCursor: "c2")
        XCTAssertTrue(state.labeled.contains(14_155_551_111))
        XCTAssertFalse(state.blocked.contains(14_155_551_111))
        XCTAssertTrue(state.blocked.isDisjoint(with: state.labeled))
    }

    // MARK: - Number moves block -> label -> unblock across separate pages/deltas

    func test_numberMoves_blockThenLabelThenUnblock_acrossDeltas() {
        var state = BlocklistState()
        let n: Int64 = 14_155_551_111

        state = state.applying([entry("+14155551111", "block", name: "First Name")], newCursor: "c1")
        XCTAssertEqual(state.blocked, [n])
        XCTAssertEqual(state.labeled, [])
        XCTAssertEqual(state.names[n], "First Name")
        XCTAssertEqual(state.cursor, "c1")

        state = state.applying([entry("+14155551111", "label", name: "Second Name")], newCursor: "c2")
        XCTAssertEqual(state.blocked, [])
        XCTAssertEqual(state.labeled, [n])
        XCTAssertEqual(state.names[n], "Second Name")
        XCTAssertEqual(state.cursor, "c2")

        state = state.applying([entry("+14155551111", "unblock")], newCursor: "c3")
        XCTAssertEqual(state.blocked, [])
        XCTAssertEqual(state.labeled, [])
        XCTAssertNil(state.names[n])
        XCTAssertEqual(state.cursor, "c3")
    }

    // MARK: - Unblock (tombstone) removes from both sets and drops name

    func test_unblock_removesFromBothSets_andDropsName() {
        let state = BlocklistState(
            blocked: [14_155_551_111],
            labeled: [14_155_552_222],
            names: [14_155_551_111: "A", 14_155_552_222: "B"],
            cursor: "c0"
        )

        let next = state.applying([
            entry("+14155551111", "unblock"),
            entry("+14155552222", "unblock"),
        ], newCursor: "c1")

        XCTAssertEqual(next.blocked, [])
        XCTAssertEqual(next.labeled, [])
        XCTAssertTrue(next.names.isEmpty)
        XCTAssertEqual(next.cursor, "c1")
    }

    // MARK: - Name set only when entry carries a non-nil name

    func test_nameNotSet_whenEntryNameIsNil() {
        let state = BlocklistState()
        let next = state.applying([entry("+14155551111", "block", name: nil)], newCursor: "c1")

        XCTAssertEqual(next.blocked, [14_155_551_111])
        XCTAssertNil(next.names[14_155_551_111])
    }

    func test_existingName_preserved_whenLaterEntryHasNilName() {
        let state = BlocklistState(blocked: [14_155_551_111], labeled: [], names: [14_155_551_111: "Original"], cursor: "c0")
        // A later "block" entry with no name shouldn't erase a previously known name.
        let next = state.applying([entry("+14155551111", "block", name: nil)], newCursor: "c1")

        XCTAssertEqual(next.names[14_155_551_111], "Original")
    }

    // MARK: - Unparseable numbers are skipped without corrupting the rest

    func test_unparseableNumber_isSkipped_restStillApplied() {
        let state = BlocklistState()
        let entries = [
            entry("not-a-number", "block", name: "Bad"),
            entry("+14155551111", "block", name: "Good"),
        ]

        let next = state.applying(entries, newCursor: "c1")

        XCTAssertEqual(next.blocked, [14_155_551_111])
        XCTAssertEqual(next.names, [14_155_551_111: "Good"])
        XCTAssertEqual(next.cursor, "c1")
    }

    // MARK: - Entries applied in order (later entry in same delta wins)

    func test_entriesAppliedInOrder_laterEntryWins() {
        let state = BlocklistState()
        let entries = [
            entry("+14155551111", "block", name: "First"),
            entry("+14155551111", "label", name: "Second"),
        ]

        let next = state.applying(entries, newCursor: "c1")

        XCTAssertEqual(next.blocked, [])
        XCTAssertEqual(next.labeled, [14_155_551_111])
        XCTAssertEqual(next.names[14_155_551_111], "Second")
    }

    // MARK: - Cursor default

    func test_defaultState_hasEmptyCursorAndEmptyCollections() {
        let state = BlocklistState()
        XCTAssertEqual(state.cursor, "")
        XCTAssertTrue(state.blocked.isEmpty)
        XCTAssertTrue(state.labeled.isEmpty)
        XCTAssertTrue(state.names.isEmpty)
    }

    // MARK: - Codable + Equatable

    func test_codableRoundTrip_preservesAllFields() throws {
        let state = BlocklistState(
            blocked: [14_155_551_111, 14_155_553_333],
            labeled: [14_155_552_222],
            names: [14_155_551_111: "A", 14_155_552_222: "B"],
            cursor: "1690000000.100"
        )

        let data = try JSONEncoder().encode(state)
        let decoded = try JSONDecoder().decode(BlocklistState.self, from: data)

        XCTAssertEqual(decoded, state)
    }
}
