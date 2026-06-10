import SwiftUI
import XClawCore

/// Bot configuration editor (opened via Settings / Cmd-,). Lists configured
/// bots, lets you add/remove and edit id / apiUrl / driver / token, and saves to
/// ~/.xclaw. Token is written to the per-bot config (plaintext for now).
struct ConfigEditorView: View {
    @Bindable var model: AppModel
    @State private var selection: String?

    var body: some View {
        NavigationSplitView {
            List(selection: $selection) {
                Section("Bots") {
                    ForEach($model.configBots) { $bot in
                        Text(bot.id.isEmpty ? "(unnamed)" : bot.id).tag(bot.id)
                    }
                    .onDelete { idx in
                        let ids = idx.map { model.configBots[$0].id }
                        ids.forEach { model.removeConfigBot($0) }
                    }
                }
            }
            .frame(minWidth: 180)
            .toolbar {
                ToolbarItem {
                    Button {
                        model.addConfigBot()
                        selection = model.configBots.last?.id
                    } label: { Image(systemName: "plus") }
                }
            }
        } detail: {
            if let sel = selection, let i = model.configBots.firstIndex(where: { $0.id == sel }) {
                BotForm(bot: $model.configBots[i])
            } else {
                Text("Select or add a bot")
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .frame(width: 620, height: 420)
        .safeAreaInset(edge: .bottom) { footer }
    }

    private var footer: some View {
        HStack {
            if let err = model.configError {
                Label(err, systemImage: "exclamationmark.triangle")
                    .font(.caption).foregroundStyle(.red).lineLimit(2)
            } else if model.needsRestart {
                Label("Saved. Restart the core to apply.", systemImage: "checkmark.circle")
                    .font(.caption).foregroundStyle(.secondary)
            }
            Spacer()
            Button("Save") { model.saveConfig() }
                .keyboardShortcut("s", modifiers: .command)
            Button("Save & Restart") {
                if model.saveConfig() { model.applyConfigAndRestart() }
            }
            .buttonStyle(.borderedProminent)
        }
        .padding(12)
        .background(.bar)
    }
}

private struct BotForm: View {
    @Binding var bot: BotConfig

    var body: some View {
        Form {
            Section("Identity") {
                TextField("Bot ID", text: $bot.id)
                    .help("Letters, digits, dot, underscore, hyphen. Used as the subtree name under ~/.xclaw.")
                if !ConfigStore.validSlug(bot.id) {
                    Text("Invalid id — letters/digits/._- only")
                        .font(.caption).foregroundStyle(.red)
                }
            }
            Section("Octo") {
                TextField("API URL", text: $bot.apiURL)
                    .textContentType(.URL)
                SecureField("Bot Token (bf_…)", text: $bot.octoToken)
                    .help("Stored in ~/.xclaw/\(bot.id)/config.json (plaintext for now).")
            }
            Section("Agent") {
                Picker("Driver", selection: $bot.driver) {
                    Text("Claude").tag("claude")
                    Text("Codex").tag("codex")
                }
                .pickerStyle(.segmented)
            }
        }
        .formStyle(.grouped)
        .padding()
    }
}
