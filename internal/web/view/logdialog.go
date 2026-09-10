package view

import (
	"fmt"
	"strconv"
)

// LogForm is the log-time dialog for one company.
type LogForm struct {
	CompanyID   int64
	CompanyName string
	// Today is the user's date: the day the dialog starts on, and the latest
	// day its picker offers.
	Today string
}

// LogSignals is the log-time dialog's fields as datastar signals. The dialog is
// seeded from it and the handler decodes the request into it, so the names on
// the two sides cannot drift apart.
type LogSignals struct {
	Task     string `json:"logTask"`
	Duration string `json:"logDuration"`
	// Date is a day in tracking.DateLayout.
	Date string `json:"logDate"`
	// At is the time of day the work started, "15:04". Empty means it just ended.
	At string `json:"logAt"`
}

// Signals is the dialog's starting state: every field blank, on today.
func (f LogForm) Signals() LogSignals { return LogSignals{Date: f.Today} }

// LogOpenerID is the DOM id of a company's Log time button, where focus goes
// back when the dialog closes.
func LogOpenerID(companyID int64) string { return "log-open-" + strconv.FormatInt(companyID, 10) }

// openLogAction asks the server for a company's log-time dialog. It needs no
// signals, so it sends none.
func openLogAction(companyID int64) string {
	return fmt.Sprintf("@get('/companies/%d/log', {filterSignals: {include: /^$/}})", companyID)
}

// closeLogAction asks the server to take the dialog away.
func closeLogAction(companyID int64) string {
	return fmt.Sprintf("@get('/companies/%d/log/close', {filterSignals: {include: /^$/}})", companyID)
}

// logAction sends the dialog's fields and nothing else: not the task boxes on
// the company cards behind it.
func logAction(companyID int64) string {
	return fmt.Sprintf("@post('/companies/%d/entries', {filterSignals: {include: /^log/}})", companyID)
}
