import XCTest
@testable import SpamFilterKit

final class BlocklistStoreTests: XCTestCase {
    private var tempDirectory: URL!

    override func setUp() {
        super.setUp()
        // Always a throwaway temp dir -- never the real App Group container.
        tempDirectory = FileManager.default.temporaryDirectory
            .appendingPathComponent("BlocklistStoreTests-\(UUID().uuidString)")
    }

    override func tearDown() {
        try? FileManager.default.removeItem(at: tempDirectory)
        tempDirectory = nil
        super.tearDown()
    }

    // MARK: - save -> load round trip

    func test_saveThenLoad_roundTripsExactState() throws {
        let store = BlocklistStore(directory: tempDirectory)
        let state = BlocklistState(
            blocked: [14_155_551_111],
            labeled: [14_155_552_222],
            names: [14_155_551_111: "A", 14_155_552_222: "B"],
            cursor: "1690000000.100"
        )

        try store.save(state)
        let loaded = store.load()

        XCTAssertEqual(loaded, state)
    }

    // MARK: - load with no file

    func test_load_withNoFile_returnsEmptyState() {
        let store = BlocklistStore(directory: tempDirectory)

        let loaded = store.load()

        XCTAssertEqual(loaded, BlocklistState())
    }

    // MARK: - load with corrupt JSON

    func test_load_withCorruptJSON_returnsEmptyState_doesNotThrow() throws {
        try FileManager.default.createDirectory(at: tempDirectory, withIntermediateDirectories: true)
        let fileURL = tempDirectory.appendingPathComponent("blocklist.json")
        try Data("{ not valid json at all".utf8).write(to: fileURL)

        let store = BlocklistStore(directory: tempDirectory)
        let loaded = store.load()

        XCTAssertEqual(loaded, BlocklistState())
    }

    // MARK: - save creates the directory if needed

    func test_save_createsDirectoryIfMissing() throws {
        XCTAssertFalse(FileManager.default.fileExists(atPath: tempDirectory.path))
        let store = BlocklistStore(directory: tempDirectory)

        try store.save(BlocklistState(blocked: [1], labeled: [], names: [:], cursor: "c"))

        XCTAssertTrue(FileManager.default.fileExists(atPath: tempDirectory.path))
    }

    // MARK: - save is atomic: an overwrite never leaves a partial/stale file

    func test_save_overwritingWithSmallerState_leavesNoStaleBytes() throws {
        let store = BlocklistStore(directory: tempDirectory)

        // First write a large state...
        let large = BlocklistState(
            blocked: Set((0..<500).map { Int64(10_000_000_000) + Int64($0) }),
            labeled: [],
            names: [:],
            cursor: "large"
        )
        try store.save(large)

        // ...then overwrite with a much smaller one.
        let small = BlocklistState(blocked: [1], labeled: [], names: [:], cursor: "small")
        try store.save(small)

        let fileURL = tempDirectory.appendingPathComponent("blocklist.json")
        let onDiskData = try Data(contentsOf: fileURL)
        let expectedData = try JSONEncoder().encode(small)

        // The file on disk must be exactly the new (smaller) encoding -- no
        // leftover bytes appended from the previous, larger write.
        XCTAssertEqual(onDiskData.count, expectedData.count)
        XCTAssertEqual(store.load(), small)

        // No stray temp files should be left behind in the directory.
        let contents = try FileManager.default.contentsOfDirectory(atPath: tempDirectory.path)
        XCTAssertEqual(contents, ["blocklist.json"])
    }

    // MARK: - App Group factory never crashes when the container is unavailable

    func test_makeAppGroupStore_whenResolverReturnsNil_throwsRatherThanCrashing() {
        // `FileManager.containerURL(forSecurityApplicationGroupIdentifier:)`
        // doesn't validate the entitlement in-process -- it constructs a
        // path even for a nonexistent identifier -- so the nil branch (the
        // real-world "entitlement missing at runtime" case) is exercised
        // here via an injected resolver instead of the real FileManager API.
        XCTAssertThrowsError(
            try BlocklistStore.makeAppGroupStore(appGroupIdentifier: "group.missing", containerResolver: { _ in nil })
        ) { error in
            XCTAssertEqual(error as? BlocklistStore.FactoryError, .appGroupContainerUnavailable(identifier: "group.missing"))
        }
    }

    func test_makeAppGroupStore_whenResolverReturnsURL_succeeds() throws {
        let store = try BlocklistStore.makeAppGroupStore(appGroupIdentifier: "group.present", containerResolver: { _ in self.tempDirectory })

        try store.save(BlocklistState(blocked: [1], labeled: [], names: [:], cursor: "c"))
        XCTAssertEqual(store.load(), BlocklistState(blocked: [1], labeled: [], names: [:], cursor: "c"))
    }
}
