## Writing

Applies to commit messages, PR bodies, code comments, and docs.

Say what changed and why. Nothing else. No summary of the work, no reflection
on it, no statement of its significance.

**Banned constructions.** Each is a tell:

- Reversals. "Pinned, not changed." "A page, not a sheet." "That is it
  working, not failing." State the thing; drop the contrast.
- Dramatic negatives. "And ran nothing." "With nothing to say why."
- Aphorisms. "A browse nobody is watching is a radio left on." "A line a
  second is a line nobody reads."
- Fragments for emphasis. "No own ship, no AIS, no laylines." Write the
  sentence.
- Personified subjects, especially with a conditional. "The seam would return
  them." "The fix buys us."
- Filler verbs where a plain one exists: carries, takes, brings, lands, sits.
  Use has, is, goes in.
- Editorial asides. "So being right matters." "Which is the contract this
  protects." "Worth noting."
- Compressed equivalence. "A change to the writers is a recapture."
- Lists of three built for rhythm rather than because there are three things.
- Anthropomorphized code. Functions return, not answer. Code does not know,
  say, want, decide, or refuse. Name the mechanism.

**Voice.** US English. Present tense for what the code does, past for what it
did. Short sentences. No em dashes. No semicolon joining two clauses for
effect.

**Commit messages.** Subject under 72 characters. Body wrapped at 72, long
enough for what changed and the one fact a reviewer needs. Three short
paragraphs is a long message. No attribution trailers, no session links, no
references to gitignored paths.

**PR bodies.** No hard wrapping: one paragraph per line. Match the section
headings of the most recent PR in the repo.

**Before publishing**, grep for: `—`, `, not `, `nothing`, `would`, `carries`,
`takes`, `worth`, `which is`. Justify each hit or cut it.

## Scope

Do what was asked. Do not add ignore rules, rename things, or fix adjacent
problems that were not part of the request. Ask instead.

Do not name platforms, tools, or experiments that are not part of the change.

## Claims

Do not write a fact you have not checked this session. If it cannot be
checked, say so in the sentence.

