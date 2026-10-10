# DuckChat CLI browser retry extension

**Legacy diagnostic only.** This CLI version no longer uses or packages this
extension. You can remove an earlier installation from `chrome://extensions`.

This extension lets the CLI make one final Duck.ai request through Chrome when its own browser retries receive HTTP 429 or cannot capture a new proof. It clears Duck.ai site data in Chrome, reloads the page, obtains a fresh request proof from that page, and relays the response to the CLI. Chrome sends the page's cookies itself; the extension does not read the cookie API or browser profile files.

## Install in Chrome

1. Open `chrome://extensions`.
2. Turn on **Developer mode**.
3. Choose **Load unpacked** and select this `duckchat-relay` directory.
4. Keep the extension enabled. A retry clears Duck.ai cookies, so it may sign you out of Duck.ai.

If you installed an earlier version from this directory, click **Reload** on its card in `chrome://extensions` after updating the CLI. Keep only one enabled copy of this extension. The CLI now opens Duck.ai directly in the same tab; it does not look up an extension ID.

The CLI opens Duck.ai after its local browser retries fail. The extension removes only `duck.ai` cache, Cache Storage, IndexedDB, local storage, service workers, and cookies, plus the current tab's session storage. This matches the selected site-data cleanup in Chrome and removes Duck.ai data in your regular Chrome profile. It does not delete data for other sites. If the browser opened by the CLI does not have this extension enabled, the CLI reports that the browser retry could not start.

## Remove

Open `chrome://extensions`, find **DuckChat CLI browser retry**, and choose **Remove**. The CLI's regular request flow does not depend on this extension.

## Permissions

The extension uses `scripting` to run the retry in the Duck.ai page's main world and `browsingData` to clear the `https://duck.ai` origin. Its host access is limited to `https://duck.ai/*` and the CLI's temporary `http://127.0.0.1` relay. It does not use Chrome's cookies API, inspect profile files, store request data, or send it to another server.
