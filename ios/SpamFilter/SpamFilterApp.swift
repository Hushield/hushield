import SwiftUI

@main
struct SpamFilterApp: App {
    @State private var environment = AppEnvironment()

    var body: some Scene {
        WindowGroup {
            ContentView(environment: environment)
        }
    }
}
