# FAQ

Short answers to the questions that keep coming back. If a question
belongs in `spec.md` or `CONTRIBUTING.md`, answer it there and link from
here instead of duplicating the text.

## How do I report a security problem?

Privately, using the path in
[`SECURITY.md`](https://github.com/<<OWNER>>/<<REPOSITORY>>/blob/main/SECURITY.md).
Never open a public issue for an exploitable vulnerability.

## Which branch do I work on?

Never `main`. Branch from `main` using
`<<type>>/<<issue number>>-<<short description>>`, and land the change
through a pull request that links its issue.

## My pull request is failing a check — where do I start?

Run the verification command locally first:

```sh
<<VERIFICATION COMMAND>>
```

The check output names the failing rule. If the check itself looks wrong,
open an issue rather than bypassing it; local hooks and CI are the merge
gates and bypassing them is visible in the record.

## A wiki page is out of date.

Open an issue. RepoForge-generated wiki pages come from `docs/wiki/` in
this repository; a hand edit is lost at the next sync and hides the real
fix.

## Something in `spec.md` is wrong.

Same answer: open an issue. `spec.md` is normative, so a change to it is
a change to the product's contract and needs the same review as code.
