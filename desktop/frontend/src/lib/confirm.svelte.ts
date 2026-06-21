// confirm() — a programmatic shim around <Confirm>. Mounts the dialog onto
// document.body, awaits the user's choice, then unmounts. Replaces the
// state-machine + ask()/answer() boilerplate that every caller had reimplemented.
//
// Use:
//   import { confirm } from "../confirm.svelte";
//   if (!await confirm({ message: "放弃改动?", danger: true })) return;
//
// The Confirm component itself is unchanged: same Esc/Enter/scrim semantics,
// same focus restore. Mounting to document.body means the dialog overlays the
// whole window — fine, since every call site sits inside a full-screen modal
// that the user can't interact with around the confirm anyway.
import { mount, unmount } from "svelte";
import Confirm from "./components/Confirm.svelte";

export interface ConfirmOpts {
  message: string;
  confirmLabel?: string;
  cancelLabel?: string;
  danger?: boolean;
}

export function confirm(opts: ConfirmOpts): Promise<boolean> {
  return new Promise((resolve) => {
    const host = document.createElement("div");
    host.className = "confirm-host";
    document.body.appendChild(host);

    const app = mount(Confirm, {
      target: host,
      props: {
        message: opts.message,
        confirmLabel: opts.confirmLabel,
        cancelLabel: opts.cancelLabel,
        danger: opts.danger ?? false,
        onresult: (ok: boolean) => {
          // Unmount on a microtask so the click handler that triggered the
          // resolve can finish first (Svelte 5 doesn't like unmounting from
          // inside a handler that's still walking the component tree).
          queueMicrotask(() => {
            unmount(app);
            host.remove();
            resolve(ok);
          });
        },
      },
    });
  });
}
