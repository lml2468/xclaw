package main

import (
	"context"

	"github.com/lml2468/octobuddy/core/clog"
	"github.com/lml2468/octobuddy/core/cron"
	"github.com/lml2468/octobuddy/core/gateway"
	"github.com/lml2468/octobuddy/core/im/octo"
	"github.com/lml2468/octobuddy/core/router"
	"github.com/lml2468/octobuddy/core/trigger"
)

// fireCronTask wakes the gateway as if a real inbound had arrived. For IM
// targets (DM/Group) it enqueues a synthetic cron-sourced message onto
// the octo connector's per-session worker so it serializes with any
// concurrent real inbound on the same sessionKey (direct gw.Handle here
// used to race onInbound's target write, mis-delivering one reply and
// dropping the other). For Console targets (ChannelConsole) the
// connector path is bypassed entirely — Console fires belong to the
// desktop GUI's CONSOLE_UID session, the IM connector has no business
// with them, and the reply naturally surfaces in the chat window via the
// existing session.user_message + session.reply event path. The Console
// call is wrapped in target.turnsWG.Add(1)/Done() so the runBot shutdown
// chain drains in-flight Console fires before st.Close.
//
// Best-effort: a failed enqueue or routing error is logged, never propagated,
// so the scheduler loop survives.
func fireCronTask(ctx context.Context, connector *octo.Connector, gw *gateway.Gateway, target *botTarget, t cron.Task) {
	cronDecision := connector.NewCronTrigger()

	if t.ChannelType == cron.ChannelConsole {
		// Console-target fire — bypass IM connector. The synthetic
		// inbound is shaped like a CONSOLE_UID DM so router.SessionKey
		// derives the same key the GUI's Composer-typed messages use,
		// and the resulting session.user_message / session.reply
		// broadcasts land in the Console session the user is watching.
		inbound := router.InboundMessage{
			FromUID:     t.FromUID,
			FromName:    t.FromName,
			ChannelType: router.ChannelDM,
			Text:        t.Prompt,
			Source:      trigger.SourceCron,
			Trigger:     cronDecision,
		}
		if _, err := inbound.SessionKey(); err != nil {
			clog.For("cron").Warn("console fire has unroutable coords", "task", t.ID, "err", err)
			return
		}
		target.turnsWG.Add(1)
		go func() {
			defer target.turnsWG.Done()
			if _, err := gw.Handle(ctx, inbound); err != nil {
				clog.For("cron").Error("console fire dispatch failed", "task", t.ID, "err", err)
			}
		}()
		return
	}

	// IM targets — the original path through the per-session worker queue.
	// Three kinds, distinguished by router type (how SessionKey derives the
	// session), octo type (how the connector addresses the send), and which
	// field carries the reply channel id:
	//
	//   Group (2)  → router Group, octo Group, channel id = t.ChannelID.
	//   Thread (5) → router Group (group-like: its own session keyed on the
	//                compound "<groupNo>____<shortId>"), octo CommunityTopic
	//                (the connector rejects a thread id queried as a plain
	//                group), channel id = t.ChannelID (the compound id).
	//   DM (1)     → router DM, octo DM, channel id = t.FromUID. An Octo DM is
	//                addressed by the PEER uid, which a DM task stores in
	//                FromUID; t.ChannelID is empty for a DM, so it must not be
	//                used as the reply target here.
	chType := router.ChannelDM
	octoType := octo.ChannelDM
	channelID := t.FromUID
	switch t.ChannelType {
	case cron.ChannelKind(router.ChannelGroup):
		chType = router.ChannelGroup
		octoType = octo.ChannelGroup
		channelID = t.ChannelID
	case cron.ChannelCommunityTopic:
		chType = router.ChannelGroup
		octoType = octo.ChannelCommunityTopic
		channelID = t.ChannelID
	}
	inbound := router.InboundMessage{
		FromUID:     t.FromUID,
		FromName:    t.FromName,
		ChannelID:   channelID,
		ChannelType: chType,
		Text:        t.Prompt,
		Source:      trigger.SourceCron,
		Trigger:     cronDecision,
	}
	key, err := inbound.SessionKey()
	if err != nil {
		clog.For("cron").Warn("task has unroutable coords", "task", t.ID, "err", err)
		return
	}
	connector.EnqueueCron(key, channelID, octoType, inbound)
}
