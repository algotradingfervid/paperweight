// Package mastersweb is the master CSV import wizard (design-system §8.7,
// CD-198, CD-201): Choose master, Upload, Check results, Import, under
// /m/import. The master and the file's token stay in the session until Import;
// the file itself waits in csv_upload (store.go). Which DocTypes can be imported
// and what a row means are csvio's and the masters'.
package mastersweb

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/a-h/templ"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/csvio"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/wizard"
)

const (
	base = "/m/import"

	// maxBody leaves room for the multipart framing around a full-size file.
	maxBody = csvio.MaxFileBytes + 1<<20

	msgNoFile  = "Choose a file to upload."
	msgClaimed = "This file is already being imported. Upload it again to import it again."

	// uploadID is the Upload step's FileUpload.
	uploadID = "csv-file"
)

// The wizard's steps, in order.
const (
	stepChoose = iota
	stepUpload
	stepCheck
	stepImport
)

var steps = []wizard.Step{
	{Slug: "choose", Label: "Choose master"},
	{Slug: "upload", Label: "Upload"},
	{Slug: "check", Label: "Check results"},
	{Slug: "import", Label: "Import"},
}

func stepURL(i int) string { return base + "/" + steps[i].Slug }

// Routes mounts the wizard under /m/import. Every handler checks the import
// right itself; WP3 mounts it behind AUTH's middleware, which puts the
// principal on the request (CD-12) and loads sm's session.
func Routes(env mstkit.Env, sm *scs.SessionManager) func(chi.Router) {
	h := &handlers{
		env:      env,
		importer: csvio.NewImporter(env.DB, env.Auth, env.Clock, env.Auditor),
		session:  wizard.SessionValues{SM: sm, Key: "master_import"},
		files:    store{db: env.DB, clock: env.Clock},
	}
	return func(r chi.Router) {
		r.Get(stepURL(stepChoose), h.handle(h.choosePage))
		r.Post(stepURL(stepChoose), h.handle(h.choose))
		r.Get(base+"/template", h.handle(h.template))
		r.Get(base+"/errors.csv", h.handle(h.errorRows))
		r.Get(stepURL(stepUpload), h.handle(h.uploadPage))
		r.Post(stepURL(stepUpload), h.handle(h.upload))
		r.Get(stepURL(stepCheck), h.handle(h.checkPage))
		r.Post(stepURL(stepCheck), h.handle(h.check))
		r.Get(stepURL(stepImport), h.handle(h.importPage))
		r.Post(stepURL(stepImport), h.handle(h.doImport))
	}
}

type handlers struct {
	env      mstkit.Env
	importer *csvio.Importer
	session  wizard.SessionValues
	files    store
}

type handlerFunc func(w http.ResponseWriter, r *http.Request, p auth.Principal) error

// handle runs fn as the signed-in user. A request without one carries the
// zero Principal, which AUTH refuses.
func (h *handlers) handle(fn handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.PrincipalFrom(r.Context())
		err := fn(w, r, p)
		if err == nil {
			return
		}
		if pr, ok := docscreen.Classify(err); ok && !ui.IsHTMX(r) {
			ui.Render(w, r, pr.Status, problemPage(pr.Message), nil)
			return
		}
		web.ErrorFrom(w, r, err)
	}
}

// importable lists what p may import, by label.
func (h *handlers) importable(ctx context.Context, p auth.Principal) []*doctype.DocType {
	var out []*doctype.DocType
	for _, dt := range doctype.All() {
		if dt.Importable && h.env.Auth.Can(ctx, p, dt.Name, auth.RightImport, nil) == nil {
			out = append(out, dt)
		}
	}
	slices.SortFunc(out, func(a, b *doctype.DocType) int { return cmp.Compare(a.PluralLabel, b.PluralLabel) })
	return out
}

// importableType is the Importable DocType named name.
func importableType(name string) (*doctype.DocType, bool) {
	dt, ok := doctype.Lookup(name)
	if !ok || !dt.Importable {
		return nil, false
	}
	return dt, true
}

// job is what the earlier steps left: the master and, past Upload, the file.
type job struct {
	dt    *doctype.DocType
	token string
	file  []byte
}

// load is the session's job for p, who must hold the import right. When a step
// it needs was never done it sends the browser there and ok is false. A token
// with no row for p (replaced, swept, or another user's) means Upload again.
func (h *handlers) load(w http.ResponseWriter, r *http.Request, p auth.Principal, needFile bool) (j job, ok bool, err error) {
	ctx := r.Context()
	vals := h.session.Get(ctx)
	dt, found := importableType(vals["doctype"])
	if !found {
		wizard.Respond(w, r, wizard.Finish, stepURL(stepChoose), nil)
		return job{}, false, nil
	}
	if err := h.env.Auth.Can(ctx, p, dt.Name, auth.RightImport, nil); err != nil {
		return job{}, false, err
	}
	j = job{dt: dt, token: vals["token"]}
	if !needFile {
		return j, true, nil
	}
	j.file, found, err = h.files.get(ctx, j.token, p.ID)
	if err != nil {
		return job{}, false, err
	}
	if !found {
		wizard.Respond(w, r, wizard.Finish, stepURL(stepUpload), nil)
		return job{}, false, nil
	}
	return j, true, nil
}

// advance moves to step: 303 for a plain post, and for htmx that step's
// page as a fragment with HX-Push-Url (design-system §8.3).
func (h *handlers) advance(w http.ResponseWriter, r *http.Request, p auth.Principal, step int) error {
	if !ui.IsHTMX(r) {
		wizard.Respond(w, r, wizard.Advance, stepURL(step), nil)
		return nil
	}
	w.Header().Set("HX-Push-Url", stepURL(step))
	switch step {
	case stepChoose:
		return h.choosePage(w, r, p)
	case stepUpload:
		return h.uploadPage(w, r, p)
	case stepCheck:
		return h.checkPage(w, r, p)
	}
	return h.importPage(w, r, p)
}

func model(step int, dt *doctype.DocType) wizard.Model {
	m := wizard.Model{Mode: wizard.Master, Title: "Import masters", BaseURL: base, Steps: steps, LastLabel: steps[stepImport].Label, Current: step, Furthest: step}
	if dt != nil {
		m.DocLabel = dt.PluralLabel
	}
	return m
}

// view is one step on screen.
type view struct {
	M    wizard.Model
	Bar  wizard.Bar
	Body templ.Component
}

func render(w http.ResponseWriter, r *http.Request, status int, v view) {
	ui.Render(w, r, status, stepPage(v, status != http.StatusOK), stepFrame(v))
}

func (h *handlers) choosePage(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	h.showChoose(w, r, p, http.StatusOK, h.session.Get(r.Context())["doctype"], "")
	return nil
}

func (h *handlers) showChoose(w http.ResponseWriter, r *http.Request, p auth.Principal, status int, value, msg string) {
	masters := h.importable(r.Context(), p)
	m := model(stepChoose, nil)
	opts := make([]components.Option, 0, len(masters)+1)
	opts = append(opts, components.Option{Label: "Choose a master"})
	for _, dt := range masters {
		opts = append(opts, components.Option{Value: dt.Name, Label: dt.PluralLabel})
	}
	field := components.FieldProps{ID: components.FieldID("doctype"), Name: "doctype", Label: "Master", Value: value, Error: msg}
	if msg != "" {
		m.Errors = []components.FieldErr{{ID: field.ID, Message: msg}}
	}
	render(w, r, status, view{M: m, Bar: wizard.Bar{Primary: "Continue", NoPrimary: len(masters) == 0}, Body: chooseBody(field, opts, len(masters) > 0)})
}

func (h *handlers) choose(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	ctx := r.Context()
	name := r.PostFormValue("doctype")
	if name == "" {
		h.showChoose(w, r, p, http.StatusUnprocessableEntity, "", "Master is required.")
		return nil
	}
	dt, ok := importableType(name)
	if !ok {
		h.showChoose(w, r, p, http.StatusUnprocessableEntity, "", "Master not found. Pick one from the list.")
		return nil
	}
	if err := h.env.Auth.Can(ctx, p, dt.Name, auth.RightImport, nil); err != nil {
		return err
	}
	step := map[string]string{"doctype": dt.Name}
	if h.session.Get(ctx)["doctype"] != dt.Name {
		step["token"] = "" // a file is only ever checked against its own master
	}
	h.session.Merge(ctx, step)
	return h.advance(w, r, p, stepUpload)
}

// template serves dt's import file. A DocType that isn't Importable doesn't
// exist here.
func (h *handlers) template(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	dt, ok := importableType(r.URL.Query().Get("doctype"))
	if !ok {
		return doctype.ErrNotFound
	}
	if err := h.env.Auth.Can(r.Context(), p, dt.Name, auth.RightImport, nil); err != nil {
		return err
	}
	return sendCSV(w, dt.Table+"-template.csv", csvio.Template(dt))
}

// sendCSV sends data as a download.
func sendCSV(w http.ResponseWriter, name string, data []byte) error {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	_, err := w.Write(data) //nolint:gosec // G705: CSV from csvio, sent as an attachment under a registry table name
	return err
}

func (h *handlers) uploadPage(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	j, ok, err := h.load(w, r, p, false)
	if !ok {
		return err
	}
	showUpload(w, r, http.StatusOK, j.dt, "")
	return nil
}

func showUpload(w http.ResponseWriter, r *http.Request, status int, dt *doctype.DocType, msg string) {
	m := model(stepUpload, dt)
	m.After = uploadStep(components.UploadProps{
		ID:       uploadID,
		Label:    "CSV file",
		Action:   stepURL(stepUpload),
		Hint:     "CSV, up to 5 MB. Required columns: " + strings.Join(csvio.RequiredColumns(dt), ", ") + ".",
		Error:    msg,
		Accept:   ".csv,text/csv",
		NoCamera: true,
		Target:   "#wizard",
	}, components.Link{
		Label: "Download " + dt.PluralLabel + " template",
		Href:  base + "/template?doctype=" + url.QueryEscape(dt.Name),
	})
	if msg != "" {
		m.Errors = []components.FieldErr{{ID: components.UploadInputID(uploadID), Message: msg}}
	}
	render(w, r, status, view{M: m, Bar: wizard.Bar{NoPrimary: true, Back: true}, Body: templ.NopComponent})
}

// tooLarge is NUM's refusal of a file over csvio.MaxFileBytes, asked of csvio
// once so the words stay NUM's.
var tooLarge = sync.OnceValue(func() string {
	_, err := csvio.ReadSheet(strings.NewReader(strings.Repeat(" ", csvio.MaxFileBytes+1)))
	return err.Error()
})

// upload holds the posted file and answers with Check results, or Upload
// again with the refusal. The body is parsed whole, not streamed: a post
// without htmx's header has already been parsed by AUTH's CSRF check.
func (h *handlers) upload(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	j, ok, err := h.load(w, r, p, false)
	if !ok {
		return err
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	err = r.ParseMultipartForm(maxBody) //nolint:gosec // G120: the MaxBytesReader above bounds it
	var over *http.MaxBytesError
	switch {
	case errors.As(err, &over):
		showUpload(w, r, http.StatusUnprocessableEntity, j.dt, tooLarge())
		return nil
	case errors.Is(err, http.ErrNotMultipart) && wizard.Nav(r) == "back":
		return h.advance(w, r, p, stepChoose)
	case err != nil:
		showUpload(w, r, http.StatusUnprocessableEntity, j.dt, msgNoFile)
		return nil
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		showUpload(w, r, http.StatusUnprocessableEntity, j.dt, msgNoFile)
		return nil
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, csvio.MaxFileBytes+1))
	if err != nil {
		return err
	}
	if len(data) > csvio.MaxFileBytes {
		showUpload(w, r, http.StatusUnprocessableEntity, j.dt, tooLarge())
		return nil
	}
	token, err := h.files.put(r.Context(), p.ID, data)
	if err != nil {
		return err
	}
	h.session.Merge(r.Context(), map[string]string{"token": token})
	return h.advance(w, r, p, stepCheck)
}

// outcome is a Preview or an Import as the screens need it: its report, or
// problem when no report exists because the file can't be run at all.
type outcome struct {
	report  csvio.Report
	problem string
}

func (o outcome) blocked() bool { return o.problem != "" || len(o.report.Errors) > 0 }

// outcomeOf sorts what csvio returned: a refused file is a message on the page;
// anything else (403, 503) stays an error.
func outcomeOf(rep csvio.Report, err error) (outcome, error) {
	var fe *csvio.FileError
	if errors.As(err, &fe) {
		return outcome{problem: fe.Msg}, nil
	}
	return outcome{report: rep}, err
}

func (h *handlers) preview(ctx context.Context, p auth.Principal, j job) (outcome, error) {
	rep, err := h.importer.Preview(ctx, p, j.dt, bytes.NewReader(j.file))
	return outcomeOf(rep, err)
}

func (h *handlers) checkPage(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	j, ok, err := h.load(w, r, p, true)
	if !ok {
		return err
	}
	o, err := h.preview(r.Context(), p, j)
	if err != nil {
		return err
	}
	status := http.StatusOK
	if o.problem != "" {
		status = http.StatusUnprocessableEntity
	}
	showCheck(w, r, status, j.dt, o)
	return nil
}

// showCheck is Check results. The way to Import exists only for a clean
// report: no link and no primary button otherwise.
func showCheck(w http.ResponseWriter, r *http.Request, status int, dt *doctype.DocType, o outcome) {
	m := model(stepCheck, dt)
	m.Action = stepURL(stepCheck)
	m.Warnings = o.report.Warnings
	if !o.blocked() {
		m.Furthest = stepImport
	}
	body := checkBody(summaryOf(o), cards(o.report), len(o.report.Errors) > 0)
	render(w, r, status, view{M: m, Bar: wizard.Bar{Primary: "Continue", NoPrimary: o.blocked(), Back: true}, Body: body})
}

// summaryOf is the banner above the cards.
func summaryOf(o outcome) components.BannerProps {
	switch {
	case o.problem != "":
		return components.BannerProps{Kind: components.Danger, Message: o.problem}
	case o.blocked():
		return components.BannerProps{Kind: components.Danger, Message: o.report.Summary()}
	}
	return components.BannerProps{Kind: components.Info, Message: o.report.Summary()}
}

// cards is one line per problem, "Row 14 · GSTIN · message" (design-system
// §8.7); the message is csvio's own.
func cards(rep csvio.Report) []string {
	out := make([]string, len(rep.Errors))
	for i, e := range rep.Errors {
		parts := []string{"Row " + strconv.Itoa(e.Row)}
		if e.Column != "" {
			parts = append(parts, e.Column)
		}
		out[i] = strings.Join(append(parts, e.Message), " · ")
	}
	return out
}

// errorRows is NUM-158's download: the rows of the file that have errors now,
// with their messages. A file with none has nothing to send.
func (h *handlers) errorRows(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	j, ok, err := h.load(w, r, p, true)
	if !ok {
		return err
	}
	o, err := h.preview(r.Context(), p, j)
	if err != nil {
		return err
	}
	if len(o.report.Errors) == 0 {
		wizard.Respond(w, r, wizard.Finish, stepURL(stepCheck), nil)
		return nil
	}
	sheet, err := csvio.ReadSheet(bytes.NewReader(j.file))
	if err != nil {
		return err
	}
	return sendCSV(w, j.dt.Table+"-errors.csv", csvio.ErrorRowsCSV(sheet, o.report))
}

func (h *handlers) check(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	if _, ok, err := h.load(w, r, p, false); !ok {
		return err
	}
	if wizard.Nav(r) == "back" {
		return h.advance(w, r, p, stepUpload)
	}
	return h.advance(w, r, p, stepImport) // importPage refuses a file with errors
}

// importPage is step 4, reachable only while Preview finds nothing wrong.
func (h *handlers) importPage(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	j, ok, err := h.load(w, r, p, true)
	if !ok {
		return err
	}
	o, err := h.preview(r.Context(), p, j)
	if err != nil {
		return err
	}
	if o.blocked() {
		wizard.Respond(w, r, wizard.Finish, stepURL(stepCheck), nil)
		return nil
	}
	m := model(stepImport, j.dt)
	m.Warnings = o.report.Warnings
	render(w, r, http.StatusOK, view{
		M:    m,
		Bar:  wizard.Bar{Primary: importLabel(j.dt, o.report.Ready), Back: true},
		Body: importBody(o.report.Summary()),
	})
	return nil
}

// doImport runs the import, which validates every row again in its own
// transaction: a file that went bad since the preview imports nothing and shows
// Check results with what is wrong now. The claim comes first, so two posts
// can't import one file twice (a master without a code has nothing else to
// stop the second). It is given up unless the import succeeded.
func (h *handlers) doImport(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	j, ok, err := h.load(w, r, p, true)
	if !ok {
		return err
	}
	if wizard.Nav(r) == "back" {
		return h.advance(w, r, p, stepCheck)
	}
	ctx := r.Context()
	claimed, err := h.files.claim(ctx, j.token, p.ID)
	if err != nil {
		return err
	}
	if !claimed {
		showCheck(w, r, http.StatusConflict, j.dt, outcome{problem: msgClaimed})
		return nil
	}
	rep, err := h.importer.Import(ctx, p, j.dt, bytes.NewReader(j.file))
	o, err := outcomeOf(rep, err)
	if err != nil || o.blocked() {
		if err := h.files.release(context.WithoutCancel(ctx), j.token, p.ID); err != nil {
			slog.ErrorContext(ctx, "mastersweb: release claim", "err", err) // a new upload replaces the row
		}
	}
	switch {
	case err != nil:
		return err
	case o.problem != "":
		wizard.Respond(w, r, wizard.Finish, stepURL(stepCheck), nil)
		return nil
	case o.blocked():
		showCheck(w, r, http.StatusUnprocessableEntity, j.dt, o)
		return nil
	}
	h.session.Clear(ctx)
	web.Flash(ctx, "success", importedMessage(j.dt, rep))
	// The rows are committed, so this failing is no failure of the import, and
	// the row stays claimed: it can't import again.
	if err := h.files.remove(ctx, j.token, p.ID); err != nil {
		slog.ErrorContext(ctx, "mastersweb: remove imported file", "err", err)
	}
	wizard.Respond(w, r, wizard.Finish, stepURL(stepChoose), nil)
	return nil
}

// importedMessage is the flash after an import: csvio's summary, in the
// singular for one row (CD-220).
func importedMessage(dt *doctype.DocType, rep csvio.Report) string {
	if rep.Imported == 1 {
		return "Imported 1 " + dt.Label + "."
	}
	return rep.Summary()
}

// importLabel is the primary button: "Import 214 parties".
func importLabel(dt *doctype.DocType, n int) string {
	if n == 1 {
		return "Import 1 " + midSentence(dt.Label)
	}
	return "Import " + types.FormatCount(int64(n)) + " " + midSentence(dt.PluralLabel)
}

// midSentence lower-cases a label's words for the middle of a sentence; a word
// with a capital after its first letter stays as it is ("UoMs").
func midSentence(label string) string {
	words := strings.Fields(label)
	for i, word := range words {
		if tail := word[1:]; tail == strings.ToLower(tail) {
			words[i] = strings.ToLower(word)
		}
	}
	return strings.Join(words, " ")
}
