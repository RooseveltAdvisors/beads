# Using bd for Static Reference Data

bd is primarily designed for work tracking, but can also serve as a queryable database for static reference data with some adaptations.

## Work Tracking (Primary Use Case)

Standard bd workflow:
- Issues flow through states (open → in_progress → closed)
- Priorities and dependencies matter
- Status tracking is essential
- IDs are sufficient for referencing

## Reference Databases / Glossaries (Alternative Use)

When using bd for static data (terminology, glossaries, reference information):

**Characteristics:**
- Entities are mostly static (typically always open)
- No real workflow or state transitions
- Names/titles more important than IDs
- Minimal or no dependencies

**Recommended approach:**
- Use separate database (not mixed with work tracking) to avoid confusion
- Consider dual format: maintain markdown version alongside database for name-based lookup
- Example: A terminology database could use both `terms.db` (queryable via bd) and `GLOSSARY.md` (browsable by name)

**Key difference**: Work items have lifecycle; reference entities are stable knowledge.

## When to Use This Pattern

**Good fit:**
- Technical glossaries or terminology databases
- Reference documentation that needs dependency tracking
- Knowledge bases with relationships between entries
- Structured data that benefits from queryability

**Poor fit:**
- Data that changes frequently (use work tracking pattern)
- Simple lists (markdown is simpler)
- Data that needs complex queries (use proper database)

## Limitations

**bd show requires IDs, not names:**
- `bd show term-42` works
- `bd show "API endpoint"` doesn't work
- Workaround: `bd list | grep -i "api endpoint"` to find ID first
- This is why dual format (bd + markdown) is recommended for reference data

**Searching by content:**
- Free-text `bd search "text"` matches title, description, ID, and external ref
- A query is an ID lookup only when it NAMES A BEAD: ID shape (a prefix, the last hyphen, a hyphen-free identifier) AND a prefix this database actually uses — hyphenated and underscored prefixes such as `claude-config-` and `bd_001-` included. Matching is case-SENSITIVE: only `MyProject-a3f8e9` is an ID lookup in a `MyProject` database, `myproject-a3f8e9` is prose. An ID lookup matches ID, title, and external ref, never descriptions, so naming a bead returns that bead and not every bead citing it
- Hyphenated terms (`use-after-free`, `race-condition`, `rate-limit`) are free text and DO search descriptions — UNLESS everything before the term's LAST hyphen is a prefix this database uses, in which case it is an ID lookup and does NOT scan descriptions (`bd-cli` in a `bd-` database, `beads-vscode-plugin` in a `beads-vscode-` one); use `bd list --desc-contains` for those
- Wisp, proto and molecule IDs (`bd-wisp-0laki`) are ID lookups too — `bd-wisp` is a prefix the database uses
- An ID whose prefix this database does not use — a foreign ID, or one spelled in the wrong case — is searched as free text; the id column is part of the free-text predicate, so it is still found by its ID text when the case matches, alongside anything citing it
- `bd list --desc-contains "text"` is the unconditional single-field description search (also `--notes-contains`, `--external-contains`)
- The same flags exist on `bd search`, but there they are AND-ed with the query: they narrow its matches
- For fields with no flag, use `bd sql`
- Example: `bd search "authentication"` or `bd sql "SELECT id, title FROM issues WHERE design LIKE '%authentication%'"`
