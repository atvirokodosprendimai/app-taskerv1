package view

import (
	"fmt"
	"strconv"
	"time"

	"github.com/atvirokodosprendimai/app-taskerv1/internal/tracking"
)

// EntryForm is the dialog for one entry: its task name, and deleting it.
type EntryForm struct {
	EntryID int64
	Company string
	// When says when the entry ran, "Thu 10 Sep, 09:00 – now", so one of a
	// company's entries can be told from another.
	When string
	// Task is the name as it stands, which the dialog opens with.
	Task string
	// FromHistory marks a dialog opened on the history page. Its actions then
	// send the filter on screen, so the results can be rendered again.
	FromHistory bool
}

// NewEntryForm is the dialog for e, with times shown in loc and a running entry
// described as of now.
func NewEntryForm(e tracking.Entry, now time.Time, loc *time.Location, fromHistory bool) EntryForm {
	start := e.StartedAt.In(loc)
	return EntryForm{
		EntryID:     e.ID,
		Company:     e.CompanyName,
		When:        dayLabel(start, now.In(loc)) + ", " + entryRange(start, e.End(now).In(loc), e.Running()),
		Task:        e.Task,
		FromHistory: fromHistory,
	}
}

// TaskSignals is the entry dialog's one field as a datastar signal. The dialog
// is seeded from it and the handler decodes the request into it, so the names on
// the two sides cannot drift apart.
type TaskSignals struct {
	Task string `json:"entryTask"`
}

// Signals is the dialog's starting state: the name as it stands. The handler
// sends it as a signal patch rather than writing it into the dialog's
// data-signals, because datastar compiles that attribute as an expression and
// rewrites @name( even inside a quoted string: a task called "Call @Anna(x)"
// would keep the dialog from opening.
func (f EntryForm) Signals() TaskSignals { return TaskSignals{Task: f.Task} }

// UndoID is the DOM id of the Undo button a deletion's confirmation carries.
const UndoID = "undo"

// EntryOpenerID is the DOM id of an entry's Edit button, where focus goes back
// when its dialog closes. A page shows an entry once, so the id is unique on it.
func EntryOpenerID(entryID int64) string { return "entry-open-" + strconv.FormatInt(entryID, 10) }

// EntryName names an entry in a sentence: its task in quotes and its company, or
// the company alone when it has no task.
func EntryName(e tracking.Entry) string {
	if e.Task == "" {
		return "an entry for " + e.CompanyName
	}
	return "“" + e.Task + "” for " + e.CompanyName
}

// RestoreAction undoes the deletion of an entry, from the page it was deleted on.
func RestoreAction(entryID int64, fromHistory bool) string {
	return fmt.Sprintf("@post('%s', {filterSignals: {include: %s}})",
		entryPath(entryID, "restore", fromHistory), pageSignals(fromHistory))
}

// openEntryAction asks the server for an entry's dialog. It sends no signals.
func openEntryAction(entryID int64, fromHistory bool) string {
	return fmt.Sprintf("@get('%s', {filterSignals: {include: /^$/}})", entryPath(entryID, "edit", fromHistory))
}

// closeEntryAction asks the server to take the dialog away.
func closeEntryAction(entryID int64) string {
	return fmt.Sprintf("@get('%s', {filterSignals: {include: /^$/}})", entryPath(entryID, "edit/close", false))
}

// saveTaskAction sends the dialog's field — with the filter, on the history
// page — and nothing else: not the task boxes on the company cards behind it.
func saveTaskAction(f EntryForm) string {
	include := "/^entryTask$/"
	if f.FromHistory {
		include = "/^(entryTask$|hist)/"
	}
	return fmt.Sprintf("@post('%s', {filterSignals: {include: %s}})", entryPath(f.EntryID, "task", f.FromHistory), include)
}

// deleteEntryAction deletes the dialog's entry.
func deleteEntryAction(f EntryForm) string {
	return fmt.Sprintf("@post('%s', {filterSignals: {include: %s}})",
		entryPath(f.EntryID, "delete", f.FromHistory), pageSignals(f.FromHistory))
}

// entryPath is the address of one of an entry's actions, marked when the action
// is taken on the history page.
func entryPath(entryID int64, action string, fromHistory bool) string {
	p := "/entries/" + strconv.FormatInt(entryID, 10) + "/" + action
	if fromHistory {
		p += "?from=history"
	}
	return p
}

// pageSignals is what an entry action sends for the page it is taken on: the
// history's filter, or nothing from the dashboard.
func pageSignals(fromHistory bool) string {
	if fromHistory {
		return "/^hist/"
	}
	return "/^$/"
}

// entryEditLabel completes an Edit button's accessible name, so a screen reader
// hears which of several identical buttons this is.
func entryEditLabel(company, task string) string {
	if task == "" {
		return " the entry for " + company
	}
	return " the entry for " + company + ": " + task
}
