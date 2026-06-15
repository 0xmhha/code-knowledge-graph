---
description: Consolidate/organize docs/ under the 3-tier discipline (plan first, then apply)
---

Consolidate and organize the documentation in `docs/`, following this repo's
documentation discipline (see `CLAUDE.md` and `docs/DOC-MAP.md`).

Scope hint (optional): $ARGUMENTS
(If empty, review all of `docs/`. If a path/topic is given, restrict to it.)

## Rules — apply strictly

1. **Ground truth = code + git.** For any claim about *current* state, verify
   against the tree and `git log` / `git -L`, cite `file:line`, and report which
   doc is stale. Never trust doc prose over code. If you cannot confirm
   something from code, mark it "unverified" — do not guess.

2. **Tier 1 is read-only input.** `docs/VISION.md` must NOT be deleted or shrunk.
   If purpose/vision prose is scattered in other docs, MOVE it into VISION.md —
   never drop it.

3. **Decisions: supersede, don't delete.** A changed decision becomes a NEW ADR
   in `docs/adr/` with the old one marked `Superseded by ADR-NNNN` (one-line
   reason). Old design docs move to `docs/archive/` with a "superseded by X"
   note — they are not deleted.

4. **Don't proliferate docs.** Prefer updating an existing Tier 2/3 doc over
   creating a new `.md`. A new file is justified only for a genuinely new
   decision (→ ADR) or a new dated status snapshot (Tier 3).

5. **Keep the index honest.** Every add / move / supersede updates
   `docs/DOC-MAP.md` (and the ADR index) in the same change.

## Procedure

1. **Discover & verify.** Identify the docs in scope. For each pair that covers
   the same topic, detect conflicts and resolve each against code+git.
2. **Plan first (do NOT mutate yet).** Present a table:

   | Doc | Tier | Action (keep / update / merge-into-X / move-to-archive / supersede-by-ADR) | Reason (+ code evidence for staleness) |

   List separately: (a) any vision/purpose prose found outside VISION.md that
   must be preserved, (b) any doc-vs-code conflicts with the verified verdict.
3. **Stop and ask for approval.** Do not perform destructive moves/merges/
   deletes until the user approves the plan.
4. **Apply** the approved plan, then update `docs/DOC-MAP.md` and ADR index.
5. **Report** what changed and what (if anything) still needs human judgment.

Begin at step 1.
