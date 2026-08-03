// Package daily owns the format of the work-day summary ("daily"): the section
// titles, the item prefixes, the prose specification handed to the model, and
// the deterministic structural fallback builder.
//
// It exists because the format used to be restated in three places (the agent
// system prompt, the /daily one-shot prompt and the fallback builder) and they
// silently drifted apart. Everything that describes or produces a daily must go
// through this package, so a change lands in one place or not at all.
package daily

import (
	"fmt"
	"strings"
	"time"

	"github.com/webcloster-dev/planner/internal/domain"
)

// Item prefixes. They are part of the wire format: the TUI, Telegram markup and
// the model contract all key off these exact strings, indentation included.
const (
	// PrefixWork marks a work item under the Trabajo section.
	PrefixWork = "  - "
	// PrefixBlock marks a blocker under the Bloqueos section.
	PrefixBlock = "  # "
	// PrefixNote marks an observation under the Notas section.
	PrefixNote = "  >> "
)

// Section titles. They are rendered in bold as "**<title>:**".
const (
	TitleDaily  = "Daily"
	TitleWork   = "Trabajo"
	TitleBlocks = "Bloqueos"
	TitleNotes  = "Notas"
)

// FormatSpec is the one and only prose specification of the daily format. Both
// the agent path (cmd/planner's system prompt) and the /daily one-shot path
// embed this exact text, so the two can never describe different formats.
//
// The content is Spanish on purpose: it is model-facing instruction text and
// the daily itself is written in Spanish. Note the backtick examples force
// string concatenation — Go raw strings cannot contain a literal backtick.
const FormatSpec = `Formato del daily:

**` + TitleDaily + `:**  <FECHA>

**` + TitleWork + `:**
` + PrefixWork + `<acción concreta por tarea, en prosa nominalizada>

**` + TitleBlocks + `:**
` + PrefixBlock + `<bloqueo, si lo hay>

**` + TitleNotes + `:**
` + PrefixNote + `<observación o recomendación técnica>

Reglas:
- <FECHA> va en formato "YYYY-MM-DD MES", con el mes abreviado en español (ej: 2026-07-29 JUL).
- Prefijos exactos: "` + PrefixWork + `", "` + PrefixBlock + `", "` + PrefixNote + `".
- Los títulos de sección van en negrita, tal cual el formato: **` + TitleDaily + `:**, **` +
	TitleWork + `:**, **` + TitleBlocks + `:**, **` + TitleNotes + `:**.
- Los ítems de ` + TitleWork + ` usan el prefijo "` + PrefixWork + `" (guion). NUNCA uses "+": el "+" queda
  reservado para las menciones de proyectos (+slug), así que confunde si aparece en el texto.
- No copies los títulos de las tareas tal cual: reformulálos como acciones concretas y claras.
  No inventes tareas que no estén en la lista.
- Rodeá con backticks toda referencia a un proyecto, documento o acción concreta
  (ej: ` + "`+liquida`, `migración de DNS`, `README.md`, `deploy a producción`" + `).
- Poné en negrita los títulos de tareas o proyectos que menciones, con **...**.
- Poné en itálica las observaciones y comentarios (el contenido de ` + TitleNotes + `), con __...__.
- Si una sección no tiene contenido, omitila por completo (incluyendo su título).`

// Prompt is the one-shot system prompt used by the /daily command. It is the
// task framing composed with FormatSpec — never a second copy of the format.
//
// Unlike the agent path, /daily injects an already-formatted <FECHA>, so it
// pins the value instead of letting the model derive it.
const Prompt = `Sos un asistente que redacta el "daily" de trabajo de un desarrollador a partir de sus tareas del día.
Escribí en español neutro-profesional, en prosa nominalizada (ej: "Identificación de anomalías en la ejecución de CRONs...", "Validación del proceso de migración...").

Devolvé EXACTAMENTE el formato especificado a continuación:

` + FormatSpec + `

Además: usá <FECHA> tal como te la paso, sin reformatearla.`

// spanishMonths are the abbreviations used in the daily header.
var spanishMonths = [...]string{"ENE", "FEB", "MAR", "ABR", "MAY", "JUN", "JUL", "AGO", "SEP", "OCT", "NOV", "DIC"}

// Date formats a day as the canonical <FECHA> of the daily header:
// "2006-01-02 MES" with a Spanish month abbreviation, e.g. "2026-07-29 JUL".
func Date(t time.Time) string {
	return t.Format("2006-01-02") + " " + spanishMonths[int(t.Month())-1]
}

// Build is the deterministic fallback digest, used when the LLM call fails so
// the user still gets something usable instead of nothing.
//
// It is deliberately STRUCTURAL ONLY: it emits the header, the section titles
// and the exact prefixes, and nothing else FormatSpec asks for. The remaining
// rules — backticked references, bold task titles, italicised notes, prose
// nominalisation — require judgement about what a task actually means and
// cannot be derived from a row in the database. Faking them (wrapping every
// title in backticks, say) would produce output that looks compliant while
// being wrong, which is worse than output that is visibly mechanical. Callers
// should treat this as a degraded mode, not as an equivalent of the model path.
func Build(date string, tasks []domain.Task) string {
	var b strings.Builder
	b.WriteString("**" + TitleDaily + ":**  " + date + "\n")
	var work, notes []string
	for _, t := range tasks {
		if t.Status == domain.StatusCancelled {
			continue
		}
		code := ""
		if t.WorkItemSeq > 0 {
			code = fmt.Sprintf("#%d ", t.WorkItemSeq)
		}
		work = append(work, fmt.Sprintf("[%s] %s%s", t.Type, code, t.Title))
		if n := strings.TrimSpace(t.Details.TechNotes); n != "" {
			notes = append(notes, n)
		}
	}
	section := func(title, prefix string, items []string) {
		if len(items) == 0 {
			return
		}
		b.WriteString("\n**" + title + ":**\n")
		for _, it := range items {
			b.WriteString(prefix + it + "\n")
		}
	}
	// The deterministic fallback fills Trabajo and Notas; Bloqueos is left to the
	// LLM daily (there is no "blocked" status — that lives in context).
	section(TitleWork, PrefixWork, work)
	section(TitleNotes, PrefixNote, notes)
	if len(tasks) == 0 {
		// No "hoy" here: the digest is built for whatever date was asked for,
		// and it already carries that date in its header.
		b.WriteString("\n(sin actividad registrada)")
	}
	return strings.TrimRight(b.String(), "\n")
}
