import SwiftUI

struct ContentView: View {
    var body: some View {
        VStack(spacing: 12) {
            Image(systemName: "phone.badge.checkmark")
                .font(.system(size: 56))
                .foregroundStyle(.tint)
            Text("SpamFilter")
                .font(.largeTitle.bold())
            Text("Community spam call & text filtering")
                .font(.subheadline)
                .foregroundStyle(.secondary)
        }
        .padding()
    }
}

#Preview {
    ContentView()
}
