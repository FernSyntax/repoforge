# Wiki Maintenance

The GitHub wiki is generated from the `docs/wiki/` directory in this
repository. `repoforge wiki sync` converges the hosted wiki to it.

```sh
repoforge wiki sync --repo {{ .Owner }}/{{ .ProjectName }}              # preview
repoforge wiki sync --repo {{ .Owner }}/{{ .ProjectName }} --apply      # publish
```

## Rules

- The default run is a preview: it writes nothing and pushes nothing.
- Pages outside the declared set are yours. Sync reports them and never
  modifies or deletes them.
- GitHub's own `_Sidebar.md` and `_Footer.md` are never touched.
- Pushes are never forced. If the wiki advanced since the preview, the
  apply is refused and you re-run.
- A token is passed to git through the environment. It never appears in
  a URL, a file, or an argument.

## When a page is out of date

Fix `docs/wiki/<page>.md` in this repository, open a pull request, and
re-run sync. Editing the wiki directly is lost at the next sync and
hides the real fix.

## Enabling the wiki

The RepoForge governance standard requires Issues and the wiki, so
`.repoforge/config.yml` sets `github.issues: true` and
`github.wiki: true`. If the wiki is disabled on the repository, sync
reports that as a configuration problem and tells you to enable it.
