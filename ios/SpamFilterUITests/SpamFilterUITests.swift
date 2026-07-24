import XCTest

/// Drives the app's four tabs end-to-end on a Simulator. Launched with the
/// `-uitest` argument so `AppEnvironment` wires deterministic, offline,
/// in-memory fakes (see `UITestSupport.swift` / `AppEnvironment.init()`) --
/// no live backend or network is required for any of these flows.
final class SpamFilterUITests: XCTestCase {
    private var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        app = XCUIApplication()
        app.launchArguments = ["-uitest"]
        app.launch()
    }

    override func tearDownWithError() throws {
        app = nil
    }

    /// Looks up an element by accessibility identifier regardless of its
    /// underlying accessibility trait (label, button, static text, ...).
    /// SwiftUI composite views (e.g. `Label`) don't always surface as the
    /// XCUIElementType a naive `.staticTexts[...]` lookup would expect, so
    /// matching `.any` keeps these queries robust to that.
    private func element(_ identifier: String) -> XCUIElement {
        app.descendants(matching: .any)[identifier]
    }

    // MARK: - Tab presence / navigation

    func testAllTabsExistAndNavigate() {
        let tabBar = app.tabBars.firstMatch
        XCTAssertTrue(tabBar.waitForExistence(timeout: 5))

        for title in ["Report", "Lookup", "Status", "Set up"] {
            XCTAssertTrue(tabBar.buttons[title].exists, "Missing tab: \(title)")
        }

        tabBar.buttons["Report"].tap()
        XCTAssertTrue(element("report.numberField").waitForExistence(timeout: 5))

        tabBar.buttons["Lookup"].tap()
        XCTAssertTrue(element("lookup.numberField").waitForExistence(timeout: 5))

        tabBar.buttons["Status"].tap()
        XCTAssertTrue(element("status.syncButton").waitForExistence(timeout: 5))

        tabBar.buttons["Set up"].tap()
        XCTAssertTrue(app.navigationBars["Set up"].waitForExistence(timeout: 5))
    }

    // MARK: - Report

    func testReportHappyPath() {
        app.tabBars.buttons["Report"].tap()

        let numberField = element("report.numberField")
        XCTAssertTrue(numberField.waitForExistence(timeout: 5))
        numberField.tap()
        numberField.typeText("+14155551234")

        // Pick a non-default vote/category to exercise the pickers too.
        app.segmentedControls.buttons["Not spam"].tap()
        app.buttons["Robocall"].tap()

        element("report.submit").tap()

        let badge = element("report.resultBadge")
        XCTAssertTrue(badge.waitForExistence(timeout: 5))
        XCTAssertFalse(element("report.validationError").exists)
    }

    func testReportValidationError() {
        app.tabBars.buttons["Report"].tap()

        let submit = element("report.submit")
        XCTAssertTrue(submit.waitForExistence(timeout: 5))
        submit.tap()

        let error = element("report.validationError")
        XCTAssertTrue(error.waitForExistence(timeout: 5))
        XCTAssertFalse(element("report.resultBadge").exists)
    }

    // MARK: - Lookup

    func testLookupFlow() {
        app.tabBars.buttons["Lookup"].tap()

        let numberField = element("lookup.numberField")
        XCTAssertTrue(numberField.waitForExistence(timeout: 5))
        numberField.tap()
        numberField.typeText("+14155551234")

        element("lookup.submit").tap()

        let badge = element("lookup.resultBadge")
        XCTAssertTrue(badge.waitForExistence(timeout: 5))
    }

    // MARK: - Sync

    func testStatusSyncFlow() {
        app.tabBars.buttons["Status"].tap()

        let syncButton = element("status.syncButton")
        XCTAssertTrue(syncButton.waitForExistence(timeout: 5))
        XCTAssertTrue(element("status.lastSynced").waitForExistence(timeout: 5))

        syncButton.tap()

        // The fake syncer completes instantly and never fails: the button
        // returns to its enabled, idle state and the last-synced row still
        // renders.
        let stillEnabled = NSPredicate(format: "isEnabled == true")
        expectation(for: stillEnabled, evaluatedWith: syncButton)
        waitForExpectations(timeout: 5)

        XCTAssertTrue(element("status.lastSynced").exists)
    }
}
