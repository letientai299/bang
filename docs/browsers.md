# Registering the resolver in Chrome and Firefox

bang has to be the browser's **default search engine**, not a keyword shortcut.
Keyword shortcuts all require a Tab or a Space before the query, so they cannot
express bare input like `#123`. The default engine is the only hook that sees
the address bar with no delimiter in front of it.

Start bang first by following [deploy.md][deploy], then open
<http://127.0.0.1:8111/>. That page detects your browser and shows the same
steps as below, lists the rules that are loaded, and has a box for trying a
shortcut without navigating anywhere.

## What the page does for you

The page advertises an [OpenSearch][opensearch] descriptor, so both browsers
discover the engine on their own. Neither one makes it the default, and that
last step is what makes bare input work. Expect to finish by hand in settings.

## Shortcut suggestions

The descriptor also points at a suggestions endpoint, so typing the start of a
shortcut offers the shortcuts that begin with it, straight from the live config.
A rule is offered by its literal prefix, which for `m (.+)` is `m ` and for a
pattern that starts with alternation, like `(a|b)`, is nothing at all — such a
rule is never suggested.

What lands in the dropdown differs by browser, because their suggestion formats
do:

- **Chrome** is sent the destination of a shortcut that needs no further input,
  and goes there directly when the entry is picked. Anything still waiting on
  input is offered as text to keep typing.
- **Firefox** is only ever offered the shortcut text. A URL handed to Firefox
  would come back as a query, match no rule, and end up searched for.

Both browsers gate the request on their own setting — _Autocomplete searches and
URLs_ under Chrome's Google services, _Provide search suggestions_ under Firefox
Settings → Search — and Chrome never asks in Incognito. Suggestions are cosmetic
either way: resolution and fallback do not depend on them.

Chrome also declines to ask for input it does not classify as a search, which is
the same judgement that produces the hostname prompt described below. A one-word
shortcut may therefore offer nothing until a space follows it.

The endpoint answers on the command line too, though what it returns depends on
the `User-Agent` it is asked with, per the split above:

```sh
curl -s 'http://127.0.0.1:8111/suggest?q=m'
```

## Chrome

Chrome picks up the descriptor as soon as you open the page, and files it under
_Site search_ **as inactive** — which is why nothing works yet.

1. Open <http://127.0.0.1:8111/> so Chrome sees the descriptor.
2. Go to `chrome://settings/searchEngines`.
3. Find **bang** under _Site search_ or _Inactive shortcuts_ and click
   **Activate**.
4. Open its ⋮ menu and choose **Make default**.

If it never appears, add it by hand from the same page: _Site search_ → **Add**,
with name `bang`, shortcut `bang`, and URL `http://127.0.0.1:8111/?q=%s`.

Chrome's settings UI uses `%s` for the query. The `{searchTerms}` form in the
OpenSearch descriptor is the underlying template syntax for the same thing.

The shortcut column reads `bang` because the descriptor says so. Chrome
otherwise derives a keyword from the URL and lists the engine as `127.0.0.1`.

### Single-word input opens a hostname prompt

Chrome guesses that a bare single token might be an intranet host, so a rule
like `pr` can trigger a "did you mean to go to pr" prompt instead of resolving.
Typing a trailing space before Enter forces it to be treated as a search.
Two-character shortcuts avoid the ambiguity entirely.

### Testing without touching your profile

`scripts/test_chrome.sh` launches Chrome on a separate `--user-data-dir`, so
rule changes can be tried without reconfiguring your real browser. Do the
Activate and Make default steps above once in that profile; it is kept between
runs, and `--reset` throws it away.

Chrome signs the Default Search Engine setting and reverts any copy written into
the profile from outside the browser — the "Chrome reset these settings" notice.
So the engine cannot be seeded ahead of the first launch, on any channel. Beta
and Canary behave the same; the script uses whichever channel it finds.

## Firefox

Firefox offers the engine through the address bar rather than adding it
silently.

1. Open <http://127.0.0.1:8111/>.
2. Click the **⋮⋮⋮ menu in the address bar** — or right-click the address bar —
   and choose **Add "bang"**.
3. Go to Settings → Search → **Default Search Engine** and pick **bang**.

If autodiscovery is not offered, [add the engine by hand][firefox-search]:
Settings → Search → **Add search engine**, with URL
`http://127.0.0.1:8111/?q=%s`.

Autodiscovery is fussy: Firefox rejects the descriptor unless its `ShortName`
matches the `title` of the `<link rel="search">` tag exactly, and unless the
response carries the `application/opensearchdescription+xml` content type. Both
are covered by tests, so a failure here is more likely a caching artifact —
reload with a hard refresh.

### Bare keywords must be allowed to search

Firefox only sends non-URL input to the search engine when `keyword.enabled` is
true. It is the default; if a shortcut like `pr` is being treated as a hostname,
check it in `about:config`.

### Testing without touching your profile

`scripts/test_firefox.sh` is the Firefox counterpart of the Chrome script above:
a separate `--profile`, kept between runs, with `--reset` to discard it. Add the
engine and make it the default once inside it.

The [`SearchEngines` policy][firefox-policies] would set the default without any
clicking, and since Firefox 139 it works outside the ESR channel. It is no help
on a machine whose Firefox is managed: a macOS configuration profile takes
precedence, and Firefox then ignores every `policies.json`. `about:policies`
shows which policies are actually in effect.

## Safari

Out of scope. Apple restricts the default engine to a fixed list, and the only
workaround is a third-party extension that would see every query typed into the
address bar, which defeats the point of resolving locally.

## Checking that it took

Type a shortcut from your config, then something ordinary. The shortcut should
land on its target; everything else should reach your usual search engine
unchanged. If ordinary searching breaks, a rule is matching too much — patterns
are anchored, so suspect a `.*` in one of them.

Without navigating anywhere:

```sh
curl -s 'http://127.0.0.1:8111/resolve?q=%23123'
```

`"matched":false` means no rule claimed the query and it fell through to the
fallback engine.

### The browser shows a warning about an insecure engine

bang is `http://`, not `https://`. Browsers treat loopback as a secure context,
so this does not block anything, but the settings UI may still label it.

### `#123` loses everything after the `#`

It does not. Once the browser has decided the input is a search query, `#` is
percent-encoded to `%23` on the way out, so the fragment never gets stripped.

[deploy]: ./deploy.md
[firefox-policies]:
  https://firefox-admin-docs.mozilla.org/reference/policies/searchengines/
[firefox-search]:
  https://support.mozilla.org/en-US/kb/add-or-remove-search-engine-firefox
[opensearch]: https://developer.mozilla.org/en-US/docs/Web/XML/Guides/OpenSearch
