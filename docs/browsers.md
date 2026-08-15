# Registering the resolver in Chrome and Firefox

bang has to be the browser's **default search engine**, not a keyword shortcut.
Keyword shortcuts all require a Tab or a Space before the query, so they cannot
express bare input like `!313`. The default engine is the only hook that sees
the address bar with no delimiter in front of it.

Start bang first — see [deployment.md][deployment] — then open
<http://127.0.0.1:8111/>. That page detects your browser and shows the same
steps as below, lists the rules that are loaded, and has a box for trying a
shortcut without navigating anywhere.

## What the page does for you

The page advertises an [OpenSearch][opensearch] descriptor, so both browsers
discover the engine on their own. Neither one makes it the default, and that
last step is what makes bare input work. Expect to finish by hand in settings.

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

### Single-word input opens a hostname prompt

Chrome guesses that a bare single token might be an intranet host, so a rule
like `mr` can trigger a "did you mean to go to mr" prompt instead of resolving.
Typing a trailing space before Enter forces it to be treated as a search.
Two-character shortcuts avoid the ambiguity entirely.

### Testing without touching your profile

`testprofile.sh` launches Chrome on a throwaway `--user-data-dir` with bang
already set as the default engine, and deletes the profile on exit. Use it to
try rule changes without reconfiguring your real browser.

## Firefox

Firefox offers the engine through the address bar rather than adding it
silently.

1. Open <http://127.0.0.1:8111/>.
2. Click the **⋮⋮⋮ menu in the address bar** — or right-click the address bar —
   and choose **Add "bang"**.
3. Go to Settings → Search → **Default Search Engine** and pick **bang**.

If autodiscovery is not offered, Firefox 140 and later can [add an engine by
hand][firefox-search]: Settings → Search → **Add search engine**, with URL
`http://127.0.0.1:8111/?q=%s`.

Autodiscovery is fussy: Firefox rejects the descriptor unless its `ShortName`
matches the `title` of the `<link rel="search">` tag exactly, and unless the
response carries the `application/opensearchdescription+xml` content type. Both
are covered by tests, so a failure here is more likely a caching artifact —
reload with a hard refresh.

### Bare keywords must be allowed to search

Firefox only sends non-URL input to the search engine when `keyword.enabled` is
true. It is the default; if a shortcut like `mr` is being treated as a hostname,
check it in `about:config`.

Search suggestions stay empty because bang serves no suggestions endpoint. That
is cosmetic — resolution and fallback are unaffected.

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
curl -s 'http://127.0.0.1:8111/resolve?q=p%21313'
```

`"matched":false` means no rule claimed the query and it fell through to the
fallback engine.

### The browser shows a warning about an insecure engine

bang is `http://`, not `https://`. Browsers treat loopback as a secure context,
so this does not block anything, but the settings UI may still label it.

### `gh#12` loses everything after the `#`

It does not. Once the browser has decided the input is a search query, `#` is
percent-encoded to `%23` on the way out, so the fragment never gets stripped.

[deployment]: ./deployment.md
[firefox-search]:
  https://support.mozilla.org/en-US/kb/add-or-remove-search-engine-firefox
[opensearch]: https://developer.mozilla.org/en-US/docs/Web/XML/Guides/OpenSearch
