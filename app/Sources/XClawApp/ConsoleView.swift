import SwiftUI
import AppKit
import XClawCore

/// Prettifies a raw sessionKey: the GUI console session reads as "Console";
/// DM/group keys (`dm:<space>:<uid>`, `group:<channel>`) collapse to "Kind · tail".
func sessionTitle(_ key: String, localUID: String) -> String {
    if key == localUID { return "Console" }
    let parts = key.split(separator: ":")
    if let kind = parts.first, parts.count > 1, let tail = parts.last {
        return "\(kind.capitalized) · \(tail)"
    }
    return key
}

// MARK: - Console (3-column)

/// The main window: Bots │ Conversations │ Transcript.
struct ConsoleView: View {
    @Bindable var model: AppModel

    var body: some View {
        NavigationSplitView {
            BotSidebarColumn(model: model)
                .navigationSplitViewColumnWidth(min: 160, ideal: 184, max: 240)
        } content: {
            ConversationsColumn(model: model)
                .navigationSplitViewColumnWidth(min: 240, ideal: 284, max: 360)
        } detail: {
            TranscriptDetail(model: model)
        }
        .frame(minWidth: 900, minHeight: 600)
    }
}

// MARK: - Column 1: Bots

private struct BotSidebarColumn: View {
    @Bindable var model: AppModel
    @Environment(\.openWindow) private var openWindow

    var body: some View {
        VStack(spacing: 0) {
            identityHeader
            List(selection: Binding(get: { model.selectedBotID },
                                    set: { model.selectedBotID = $0 })) {
                Section {
                    if model.roster.isEmpty {
                        Text("No bots configured")
                            .appFont(.callout).foregroundStyle(.secondary)
                    }
                    ForEach(model.roster) { item in
                        BotRow(item: item)
                            .tag(item.id)
                            .contextMenu {
                                Button {
                                    NSApp.activate(ignoringOtherApps: true)
                                    openWindow(id: "bot-editor")
                                } label: { Label("Edit Bots…", systemImage: "slider.horizontal.3") }
                                Button {
                                    model.selectedBotID = item.id; model.restartCore()
                                } label: { Label("Restart Core", systemImage: "arrow.clockwise") }
                                Divider()
                                Button(role: .destructive) {
                                    model.selectedBotID = item.id; model.reset()
                                } label: { Label("Clear Memory", systemImage: "eraser.line.dashed") }
                            }
                    }
                } header: {
                    Text("BOTS").appFont(.caption).tracking(0.6).foregroundStyle(.secondary)
                }
            }
            .listStyle(.sidebar)
            .scrollContentBackground(.hidden)
            .animation(AppTheme.spring, value: model.roster.map(\.id))
        }
        .background(.ultraThinMaterial)
    }

    /// Signature identity header — gives the app a face. The faint brand-gradient
    /// wash extends under the window's traffic lights (content padded clear of them).
    private var identityHeader: some View {
        HStack(spacing: 9) {
            OctopusShape()
                .fill(AppTheme.brandGradient, style: FillStyle(eoFill: true))
                .frame(width: 22, height: 22)
            Text("XClaw").appFont(.headline)
            Spacer()
        }
        .padding(.top, 30)   // clear the traffic lights (full-size-content title bar)
        .padding(.horizontal, 14)
        .padding(.bottom, 12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(AppTheme.brandGradient.opacity(0.10))
        .overlay(alignment: .bottom) { Divider().opacity(0.4) }
    }
}

private struct BotRow: View {
    let item: BotRosterItem
    var body: some View {
        HStack(spacing: 9) {
            Circle()
                .fill(item.connected ? Color.green : Color.secondary)
                .frame(width: 8, height: 8)
                .shadow(color: item.connected ? .green.opacity(0.5) : .clear, radius: 3)
            VStack(alignment: .leading, spacing: 1) {
                Text(item.id).appFont(.headline)
                Text(item.connected ? "connected" : (item.lastError ?? "offline"))
                    .appFont(.caption).foregroundStyle(.secondary).lineLimit(1)
            }
            Spacer(minLength: 4)
            if item.sessionCount > 0 {
                Text("\(item.sessionCount)")
                    .appFont(.caption).monospacedDigit()
                    .foregroundStyle(.secondary)
                    .padding(.horizontal, 6).padding(.vertical, 1)
                    .background(.quaternary, in: Capsule())
            }
        }
        .padding(.vertical, 3)
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
        .accessibilityLabel("\(item.id), \(item.connected ? "connected" : "offline"), \(item.sessionCount) sessions")
    }
}

// MARK: - Column 2: Conversations

private struct ConversationsColumn: View {
    @Bindable var model: AppModel

    var body: some View {
        Group {
            if let bot = model.currentBot {
                List(selection: Binding(get: { model.selectedSessionKey },
                                        set: { model.selectedSessionKey = $0 })) {
                    Section {
                        ForEach(bot.sortedSessions) { s in
                            ConversationRow(session: s, localUID: model.localUID)
                                .tag(s.sessionKey)
                        }
                    } header: {
                        Text("Conversations").appFont(.caption).foregroundStyle(.secondary)
                    }
                }
                .listStyle(.sidebar)
                .scrollContentBackground(.hidden)
            } else {
                ContentUnavailableView {
                    Label("No Bot Selected", systemImage: "tray")
                } description: {
                    Text("Pick a bot on the left.")
                }
            }
        }
        .onAppear { ensureSelection() }
        .onChange(of: model.selectedBotID) { _, _ in ensureSelection() }
    }

    /// Default the conversation to the bot's Console session when switching bots.
    private func ensureSelection() {
        let keys = model.currentBot?.sortedSessions.map(\.sessionKey) ?? []
        if model.selectedSessionKey == nil || !keys.contains(model.selectedSessionKey!) {
            model.selectedSessionKey = keys.first { $0 == model.localUID } ?? keys.first
        }
    }
}

private struct ConversationRow: View {
    let session: AppState.SessionView
    let localUID: String

    private var subtitle: String {
        if session.awaitingReply { return "replying…" }
        let last = session.messages.last?.text ?? session.lastReply
        return last.isEmpty ? "No messages yet" : last
    }

    var body: some View {
        HStack(spacing: 9) {
            Image(systemName: session.sessionKey == localUID ? "macwindow" : "bubble.left.and.bubble.right")
                .font(.system(size: 13, weight: .medium))
                .symbolRenderingMode(.hierarchical)
                .foregroundStyle(.tint)
                .frame(width: 18)
            VStack(alignment: .leading, spacing: 1) {
                Text(sessionTitle(session.sessionKey, localUID: localUID)).appFont(.headline)
                Text(subtitle).appFont(.caption).foregroundStyle(.secondary).lineLimit(1)
            }
            Spacer(minLength: 0)
            if session.awaitingReply {
                ProgressView().controlSize(.small).scaleEffect(0.7)
            }
        }
        .padding(.vertical, 4)
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
        .accessibilityLabel("\(sessionTitle(session.sessionKey, localUID: localUID)), \(subtitle)")
    }
}

// MARK: - Column 3: Transcript + composer

private struct TranscriptDetail: View {
    @Bindable var model: AppModel
    @Environment(\.openWindow) private var openWindow
    @State private var draft: String = ""
    @State private var atBottom: Bool = true
    @FocusState private var composerFocused: Bool

    private var currentMessages: [AppState.ChatMessage] { model.currentSession?.messages ?? [] }
    private var isLoadingHistory: Bool {
        guard let id = model.selectedBotID else { return false }
        return model.historyLoadingBots.contains(id) && currentMessages.isEmpty
    }

    var body: some View {
        VStack(spacing: 0) {
            banners
            transcript
        }
        .background(detailBackground)
        .safeAreaInset(edge: .bottom, spacing: 0) { composer }
        .toolbar { toolbar }
    }

    /// A very faint brand-tinted wash over the window background — subtle depth,
    /// not a loud color field (restraint).
    private var detailBackground: some View {
        LinearGradient(colors: [Color.brand.opacity(0.05), .clear],
                       startPoint: .top, endPoint: .center)
            .ignoresSafeArea()
    }

    @ViewBuilder private var banners: some View {
        if model.coreState == .needsConfig {
            InfoBanner(text: "No bots configured. Add one to get started.",
                       systemImage: "gearshape.badge.exclamationmark", tint: .orange) {
                Button("Edit Bots…") {
                    NSApp.activate(ignoringOtherApps: true)
                    openWindow(id: "bot-editor")
                }
            }
        }
        if model.config.needsRestart {
            InfoBanner(text: "Configuration changed — restart the core to apply.",
                       systemImage: "arrow.clockwise.circle", tint: .brand) {
                Button("Restart now") { model.applyConfigAndRestart() }
            }
        }
    }

    @ToolbarContentBuilder private var toolbar: some ToolbarContent {
        ToolbarItemGroup(placement: .primaryAction) {
            Button { model.reset() } label: { Image(systemName: "eraser.line.dashed") }
                .help("Clear this bot's conversation memory")
                .accessibilityLabel("Clear conversation memory")
            Button { model.restartCore() } label: { Image(systemName: "arrow.clockwise") }
                .help("Restart the xclawd core process")
                .accessibilityLabel("Restart core")
        }
    }

    private var transcript: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 10) {
                    if let err = model.lastError, !err.isEmpty {
                        Label(err, systemImage: "exclamationmark.triangle.fill")
                            .appFont(.caption).foregroundStyle(.red)
                            .textSelection(.enabled)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(10)
                            .background(.red.opacity(0.08), in: RoundedRectangle(cornerRadius: 8, style: .continuous))
                    }

                    if isLoadingHistory {
                        ForEach(0..<5, id: \.self) { i in MessageSkeleton(fromUser: i % 2 == 1, delay: Double(i) * 0.12) }
                    } else if currentMessages.isEmpty {
                        emptyState
                    } else {
                        let streamingID = model.currentSession?.streamingAssistant
                        ForEach(currentMessages) { msg in
                            ChatBubble(message: msg, streaming: streamingID == msg.id)
                                .transition(.asymmetric(
                                    insertion: .scale(scale: 0.96, anchor: .bottom).combined(with: .opacity),
                                    removal: .opacity))
                        }
                        if model.currentSession?.awaitingReply == true {
                            TypingBubble()
                                .transition(.opacity.combined(with: .scale(scale: 0.92, anchor: .leading)))
                        }
                        if let s = model.currentSession, s.outputTokens > 0 {
                            Text("\(s.inputTokens) in · \(s.outputTokens) out")
                                .appFont(.caption).monospacedDigit()
                                .foregroundStyle(.tertiary)
                                .frame(maxWidth: .infinity, alignment: .center)
                                .padding(.top, 2)
                        }
                        Color.clear.frame(height: 1).id("bottom")
                    }
                }
                .padding(20)
                .animation(AppTheme.spring, value: currentMessages.count)
            }
            .scrollContentBackground(.hidden)
            .onChange(of: currentMessages.count) { _, _ in proxy.scrollTo("bottom", anchor: .bottom) }
            .onChange(of: model.selectedSessionKey) { _, _ in
                atBottom = true
                proxy.scrollTo("bottom", anchor: .bottom)
            }
            // Follow streamed text only while parked at the bottom — never fights a
            // user who has scrolled up to read history. Cheap: observes an Int tick
            // + scroll geometry, not the (heavy) session value. (macOS 15+.)
            .modifier(StreamingFollow(tick: model.transcriptTick, atBottom: $atBottom) {
                proxy.scrollTo("bottom", anchor: .bottom)
            })
        }
    }

    /// Sample prompts shown in the empty state; tapping one fills the composer.
    private let samplePrompts = [
        "What can you help me with?",
        "Summarize the latest messages in this channel.",
        "Draft a concise status update for the team.",
    ]

    private var emptyState: some View {
        VStack(spacing: 20) {
            ZStack {
                Circle().fill(AppTheme.brandGlow).frame(width: 230, height: 230)
                OctopusShape()
                    .fill(AppTheme.brandGradient, style: FillStyle(eoFill: true))
                    .frame(width: 88, height: 88)
                    .shadow(color: Color.brand.opacity(0.35), radius: 18, y: 8)
            }
            VStack(spacing: 6) {
                Text("Talk to your agent").appFont(.largeTitle)
                Text("Ask anything below, or start with one of these.")
                    .appFont(.body).foregroundStyle(.secondary)
            }
            VStack(spacing: 9) {
                ForEach(samplePrompts, id: \.self) { prompt in
                    Button {
                        draft = prompt
                        composerFocused = true
                    } label: {
                        HStack(spacing: 8) {
                            Image(systemName: "sparkles").font(.caption).foregroundStyle(Color.brand)
                            Text(prompt).appFont(.callout).foregroundStyle(.primary)
                            Spacer(minLength: 0)
                        }
                        .frame(maxWidth: 360, alignment: .leading)
                        .padding(.horizontal, 14).padding(.vertical, 10)
                        .background(.ultraThinMaterial, in: Capsule())
                        .overlay(Capsule().strokeBorder(Color.brand.opacity(0.18), lineWidth: 1))
                        .contentShape(Capsule())
                    }
                    .buttonStyle(.plain)
                    .hoverScale(1.03)
                }
            }
            .padding(.top, 4)
        }
        .frame(maxWidth: .infinity)
        .padding(.top, 56)
    }

    private var canSend: Bool {
        model.connected && !draft.trimmingCharacters(in: .whitespaces).isEmpty
    }

    private var composer: some View {
        HStack(alignment: .bottom, spacing: 8) {
            TextField("Message the agent…", text: $draft, axis: .vertical)
                .textFieldStyle(.plain)
                .appFont(.body)
                .lineLimit(1...6)
                .focused($composerFocused)
                .onSubmit(sendDraft)
                .padding(.horizontal, 12)
                .padding(.vertical, 9)
                .background(Color(nsColor: .textBackgroundColor),
                            in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                .focusRing(composerFocused, cornerRadius: 10)
            Button(action: sendDraft) {
                Image(systemName: "arrow.up")
                    .font(.system(size: 15, weight: .bold))
                    .foregroundStyle(.white)
                    .frame(width: 30, height: 30)
                    .background(canSend ? AnyShapeStyle(AppTheme.brandGradient)
                                        : AnyShapeStyle(Color.secondary.opacity(0.35)),
                                in: Circle())
                    .shadow(color: canSend ? Color.brand.opacity(0.40) : .clear, radius: 5, y: 2)
            }
            .buttonStyle(.plain)
            .disabled(!canSend)
            .keyboardShortcut(.return, modifiers: [])
            .help("Send (Return)")
            .accessibilityLabel("Send message")
            .hoverScale(1.08)
            .animation(AppTheme.spring, value: canSend)
        }
        .padding(12)
        .background(.bar)
        .overlay(alignment: .top) { Divider().opacity(0.5) }
    }

    private func sendDraft() {
        let text = draft
        draft = ""
        model.send(text)
        composerFocused = true
    }
}

// MARK: - Components

/// A thin, glassy info bar shown above the content (needs-config, restart…).
private struct InfoBanner<Trailing: View>: View {
    let text: String
    let systemImage: String
    let tint: Color
    @ViewBuilder var trailing: Trailing

    var body: some View {
        HStack(spacing: 8) {
            Image(systemName: systemImage).foregroundStyle(tint)
            Text(text).appFont(.body)
            Spacer()
            trailing.controlSize(.small)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
        .liquidGlass(in: Rectangle(), fallback: .regularMaterial)
        .overlay(alignment: .bottom) { Divider().opacity(0.5) }
    }
}

/// One message: tool calls render as a centered chip; user/assistant as a bubble.
struct ChatBubble: View {
    let message: AppState.ChatMessage
    var streaming: Bool = false

    var body: some View {
        if message.role == .tool {
            HStack {
                Spacer()
                Label(message.text, systemImage: "wrench.and.screwdriver.fill")
                    .appFont(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .capsuleTag()
                Spacer()
            }
        } else {
            BubbleRow(message: message, streaming: streaming)
        }
    }
}

/// A 28pt chat avatar: the octopus mark for the assistant, a brand-gradient
/// person glyph for the user — gives each bubble a clear identity.
private struct AvatarView: View {
    let isUser: Bool
    var body: some View {
        ZStack {
            if isUser {
                Circle().fill(AppTheme.brandGradient)
                Image(systemName: "person.fill")
                    .font(.system(size: 12, weight: .semibold))
                    .foregroundStyle(.white)
            } else {
                Circle().fill(.thickMaterial)
                Circle().strokeBorder(Color.brand.opacity(0.25), lineWidth: 1)
                OctopusShape()
                    .fill(style: FillStyle(eoFill: true))
                    .foregroundStyle(Color.brand)
                    .frame(width: 17, height: 17)
            }
        }
        .frame(width: 28, height: 28)
        .floatingShadow()
        .accessibilityHidden(true)
    }
}

/// A user/assistant bubble that reveals a timestamp + copy button on hover.
private struct BubbleRow: View {
    let message: AppState.ChatMessage
    var streaming: Bool = false
    @State private var hovering = false

    private var isUser: Bool { message.role == .user }

    var body: some View {
        VStack(alignment: isUser ? .trailing : .leading, spacing: 2) {
            bubble
            meta
                .opacity(hovering ? 1 : 0)
                .frame(height: 14)
                .allowsHitTesting(hovering)
        }
        .frame(maxWidth: .infinity, alignment: isUser ? .trailing : .leading)
        .onHover { hovering = $0 }
    }

    private var bubble: some View {
        HStack(alignment: .top, spacing: 8) {
            if !isUser { AvatarView(isUser: false) }
            if isUser { Spacer(minLength: 40) }
            content
                .padding(.vertical, 9)
                .padding(.horizontal, 13)
                .background(isUser ? AnyShapeStyle(AppTheme.brandGradient)
                                   : AnyShapeStyle(.thickMaterial),
                            in: RoundedRectangle(cornerRadius: 16, style: .continuous))
                .overlay {
                    if !isUser {
                        RoundedRectangle(cornerRadius: 16, style: .continuous)
                            .strokeBorder(.quaternary, lineWidth: 1)
                    }
                }
                .floatingShadow()
                .contextMenu {
                    Button { copy() } label: { Label("Copy", systemImage: "doc.on.doc") }
                }
            if !isUser { Spacer(minLength: 40) }
            if isUser { AvatarView(isUser: true) }
        }
    }

    /// User text stays plain (white on the brand bubble). The in-flight assistant
    /// bubble uses a smooth typewriter reveal (bursty deltas → continuous typing);
    /// once complete it renders formatted Markdown. Width-capped for readability.
    @ViewBuilder private var content: some View {
        if isUser {
            Text(message.text).appFont(.body).textSelection(.enabled).foregroundStyle(.white)
        } else if streaming {
            TypewriterText(text: message.text)
                .frame(maxWidth: 640, alignment: .leading)
        } else {
            MarkdownMessage(text: message.text)
                .appFont(.body)
                .frame(maxWidth: 640, alignment: .leading)
        }
    }

    private func copy() {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(message.text, forType: .string)
    }

    private var meta: some View {
        HStack(spacing: 6) {
            if isUser { copyButton }
            Text(message.timestamp, format: .dateTime.hour().minute()).monospacedDigit()
            if !isUser { copyButton }
        }
        .appFont(.caption)
        .foregroundStyle(.secondary)
    }

    private var copyButton: some View {
        Button { copy() } label: { Image(systemName: "doc.on.doc") }
            .buttonStyle(.borderless)
            .help("Copy message")
            .accessibilityLabel("Copy message")
    }
}

/// Smoothly reveals streamed text at a steady per-frame rate so bursty deltas
/// (the agent emits text in chunks every few hundred ms) read as continuous
/// typing. Used only for the in-flight assistant bubble; the completed message
/// switches to formatted Markdown. The reveal eases out — it advances a fraction
/// of the backlog each frame (min 1 char) so a burst is spread, not snapped, and
/// it catches up fast enough that the hand-off to Markdown is seamless.
private struct TypewriterText: View {
    let text: String
    @State private var shown: Int = 0
    private let tick = Timer.publish(every: 1.0 / 60.0, on: .main, in: .common).autoconnect()

    var body: some View {
        Text(String(text.prefix(shown)))
            .appFont(.body)
            .textSelection(.enabled)
            .frame(maxWidth: .infinity, alignment: .leading)
            .onReceive(tick) { _ in
                let total = text.count
                guard shown < total else { return }
                let backlog = total - shown
                shown = min(total, shown + max(1, backlog / 8))
            }
            .onChange(of: text) { _, newText in
                if shown > newText.count { shown = 0 } // text replaced (new turn)
            }
            .onAppear { if shown > text.count { shown = 0 } }
    }
}

/// Animated "agent is typing" indicator shown while awaiting the first output.
struct TypingBubble: View {
    @State private var phase = 0
    private let timer = Timer.publish(every: 0.35, on: .main, in: .common).autoconnect()

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            AvatarView(isUser: false)
            HStack(spacing: 4) {
                ForEach(0..<3, id: \.self) { i in
                    Circle()
                        .fill(.secondary)
                        .frame(width: 6, height: 6)
                        .opacity(phase == i ? 1 : 0.3)
                        .scaleEffect(phase == i ? 1.15 : 1)
                        .animation(AppTheme.spring, value: phase)
                }
            }
            .padding(.vertical, 11)
            .padding(.horizontal, 15)
            .background(.thickMaterial, in: RoundedRectangle(cornerRadius: 15, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: 15, style: .continuous).strokeBorder(.quaternary, lineWidth: 1))
            .floatingShadow()
            Spacer(minLength: 56)
        }
        .onReceive(timer) { _ in phase = (phase + 1) % 3 }
        .accessibilityLabel("Agent is replying")
    }
}

/// Follows streamed transcript updates by scrolling to the bottom on each tick,
/// but only while the user is parked near the bottom (tracked via scroll geometry
/// on macOS 15+). On older macOS it's a no-op (count-based scroll still applies),
/// so it never fights a user who has scrolled up. Cheap: observes an Int tick +
/// scroll offset, never the heavy session value.
private struct StreamingFollow: ViewModifier {
    let tick: Int
    @Binding var atBottom: Bool
    let scrollToBottom: () -> Void

    func body(content: Content) -> some View {
        if #available(macOS 15.0, *) {
            content
                .onScrollGeometryChange(for: Bool.self) { geo in
                    geo.contentOffset.y + geo.containerSize.height >= geo.contentSize.height - 120
                } action: { _, nearBottom in
                    atBottom = nearBottom
                }
                .onChange(of: tick) { _, _ in
                    if atBottom { scrollToBottom() }
                }
        } else {
            content
        }
    }
}

/// A shimmering placeholder (avatar + two text lines) shown while a transcript's
/// history loads. `delay` staggers the shimmer per row.
private struct MessageSkeleton: View {
    let fromUser: Bool
    var delay: Double = 0
    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            if fromUser { Spacer(minLength: 80) }
            if !fromUser { Circle().fill(.quaternary).frame(width: 28, height: 28) }
            VStack(alignment: .leading, spacing: 6) {
                RoundedRectangle(cornerRadius: 5, style: .continuous)
                    .fill(.quaternary).frame(width: fromUser ? 150 : 240, height: 11)
                RoundedRectangle(cornerRadius: 5, style: .continuous)
                    .fill(.quaternary).frame(width: fromUser ? 100 : 170, height: 11)
            }
            .padding(.vertical, 11).padding(.horizontal, 13)
            .background(.quaternary.opacity(0.5), in: RoundedRectangle(cornerRadius: 16, style: .continuous))
            .shimmering(delay: delay)
            if fromUser { Circle().fill(.quaternary).frame(width: 28, height: 28) }
            if !fromUser { Spacer(minLength: 80) }
        }
    }
}
