import SwiftUI

/// Walkthrough for enabling the two iOS extensions, each deep-linking to
/// Settings, plus a checklist of steps.
struct SetupScreen: View {
    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: Theme.Spacing.lg) {
                    intro

                    setupStep(
                        icon: "phone.badge.checkmark",
                        color: Theme.accent,
                        title: "Call Blocking & Identification",
                        blurb: "Lets SpamFilter block and label incoming calls from the community blocklist.",
                        steps: [
                            "Open Settings \u{203A} Apps \u{203A} Phone",
                            "Tap \u{201C}Call Blocking & Identification\u{201D}",
                            "Enable SpamFilter"
                        ]
                    )

                    setupStep(
                        icon: "message.badge.filled.fill",
                        color: Theme.warn,
                        title: "Message Filtering",
                        blurb: "Routes texts from unknown senders through SpamFilter to catch spam messages.",
                        steps: [
                            "Open Settings \u{203A} Apps \u{203A} Messages",
                            "Tap \u{201C}Unknown & Spam\u{201D}",
                            "Turn on \u{201C}Filter Unknown Senders\u{201D} and select SpamFilter under SMS Filtering"
                        ]
                    )

                    Button {
                        openSettings()
                    } label: {
                        Label("Open Settings", systemImage: "gear")
                    }
                    .buttonStyle(PrimaryButtonStyle())

                    Text("iOS only lets apps deep-link to the Settings root. Follow the steps above from there.")
                        .font(.footnote)
                        .foregroundStyle(Theme.secondaryText)
                        .frame(maxWidth: .infinity, alignment: .center)
                        .multilineTextAlignment(.center)
                }
                .padding(Theme.Spacing.md)
            }
            .background(Theme.background.ignoresSafeArea())
            .navigationTitle("Set up")
        }
    }

    private var intro: some View {
        VStack(alignment: .leading, spacing: Theme.Spacing.xs) {
            Text("Turn on protection")
                .font(.title2.bold())
            Text("SpamFilter works through two iOS extensions. Enable both to block spam calls and texts.")
                .font(.subheadline)
                .foregroundStyle(Theme.secondaryText)
        }
    }

    private func setupStep(icon: String, color: Color, title: String, blurb: String, steps: [String]) -> some View {
        Card {
            VStack(alignment: .leading, spacing: Theme.Spacing.md) {
                HStack(spacing: Theme.Spacing.sm) {
                    Image(systemName: icon)
                        .font(.title2)
                        .foregroundStyle(color)
                    Text(title)
                        .font(.headline)
                }
                Text(blurb)
                    .font(.subheadline)
                    .foregroundStyle(Theme.secondaryText)
                VStack(alignment: .leading, spacing: Theme.Spacing.sm) {
                    ForEach(Array(steps.enumerated()), id: \.offset) { index, step in
                        HStack(alignment: .top, spacing: Theme.Spacing.sm) {
                            Text("\(index + 1)")
                                .font(.caption.bold())
                                .foregroundStyle(.white)
                                .frame(width: 22, height: 22)
                                .background(color, in: Circle())
                            Text(step)
                                .font(.subheadline)
                            Spacer(minLength: 0)
                        }
                    }
                }
            }
        }
    }

    private func openSettings() {
        guard let url = URL(string: UIApplication.openSettingsURLString) else { return }
        UIApplication.shared.open(url)
    }
}
