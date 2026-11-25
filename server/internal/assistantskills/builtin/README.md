# Built-in skills

Each subdirectory is a skill shipped with personad. Only skills Persona may redistribute go here (the license allows it, or we
wrote them), each registered in `Redistributable` in `embed.go` (`TestBuiltinsAreRedistributable` guards it). A skill copied
from elsewhere that may not be redistributed belongs in the user's own space, `~/.persona/skills/`
(`docs/operations/hub-update.md`, "Skills move out of the binary").
