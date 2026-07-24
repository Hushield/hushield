import SwiftUI

/// Root tab bar wiring the four screens to the shared `AppEnvironment`.
struct ContentView: View {
    let environment: AppEnvironment

    var body: some View {
        TabView {
            ReportScreen(reporting: environment.reporting)
                .tabItem { Label("Report", systemImage: "flag.fill") }

            LookupScreen(lookup: environment.lookup)
                .tabItem { Label("Lookup", systemImage: "magnifyingglass") }

            StatusScreen(syncing: environment.syncing, status: environment.statusReading)
                .tabItem { Label("Status", systemImage: "chart.bar.fill") }

            SetupScreen()
                .tabItem { Label("Set up", systemImage: "gearshape.fill") }
        }
        .tint(Theme.accent)
    }
}
