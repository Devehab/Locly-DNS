package core

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"runtime"
	"strings"

	"github.com/devehab/locly-dns/internal/config"
	"github.com/devehab/locly-dns/internal/hosts"
	"github.com/devehab/locly-dns/internal/platform"
)

// Section states reported by Status.
const (
	SectionPresent = "present"
	SectionAbsent  = "absent"
	SectionInvalid = "invalid"
)

// Counts summarizes entry statuses.
type Counts struct {
	Total    int `json:"total"`
	Active   int `json:"active"`
	Missing  int `json:"missing"`
	Conflict int `json:"conflict"`
}

// StatusReport describes the current LocalDNS state.
type StatusReport struct {
	HostsFile    string  `json:"hosts_file"`
	ConfigFile   string  `json:"config_file"`
	Section      string  `json:"section"`
	SectionError string  `json:"section_error,omitempty"`
	ConfigError  string  `json:"config_error,omitempty"`
	Writable     bool    `json:"writable"`
	Healthy      bool    `json:"healthy"`
	Counts       Counts  `json:"counts"`
	Entries      []Entry `json:"entries"`
}

// Status inspects the hosts file and config. Unlike List it does not fail on
// a malformed section or config; it reports the problem instead. It only
// fails if the hosts file cannot be read at all.
func (m *Manager) Status() (StatusReport, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	r := StatusReport{HostsFile: m.HostsPath(), ConfigFile: m.ConfigPath(), Entries: []Entry{}}
	raw, err := m.readHosts()
	if err != nil {
		return r, err
	}
	doc := hosts.Parse(raw)
	cfg, cerr := m.loadConfig()
	if cerr != nil {
		r.ConfigError = cerr.Error()
		cfg = &config.Config{}
	}
	switch serr := doc.Err(); {
	case serr != nil:
		r.Section = SectionInvalid
		r.SectionError = serr.Error()
	case doc.HasSection():
		r.Section = SectionPresent
	default:
		r.Section = SectionAbsent
	}
	if doc.Err() == nil {
		r.Entries = entries(&state{raw: raw, doc: doc, cfg: cfg})
		if r.Entries == nil {
			r.Entries = []Entry{}
		}
	}
	for _, e := range r.Entries {
		r.Counts.Total++
		switch e.Status {
		case StatusActive:
			r.Counts.Active++
		case StatusMissing:
			r.Counts.Missing++
		case StatusConflict:
			r.Counts.Conflict++
		}
	}
	r.Writable = m.checkWritable(true, true) == nil
	r.Healthy = r.Section != SectionInvalid && r.ConfigError == "" && r.Counts.Active == r.Counts.Total
	return r, nil
}

// CheckState is the outcome of one doctor check.
type CheckState string

// Doctor check states.
const (
	CheckPass CheckState = "pass"
	CheckWarn CheckState = "warn"
	CheckFail CheckState = "fail"
)

// Check is one doctor finding. Fix explains how to resolve a warning or
// failure.
type Check struct {
	ID      string     `json:"id"`
	Name    string     `json:"name"`
	Status  CheckState `json:"status"`
	Message string     `json:"message,omitempty"`
	Fix     string     `json:"fix,omitempty"`
}

// DoctorReport is the result of Doctor. OK is false if any check failed.
type DoctorReport struct {
	OK     bool    `json:"ok"`
	Checks []Check `json:"checks"`
}

// Doctor diagnoses the installation. extra checks (for example "is the UI
// port free?", which needs networking code that core must not contain) are
// appended by the caller.
func (m *Manager) Doctor(extra ...func() Check) DoctorReport {
	m.mu.Lock()
	var checks []Check
	add := func(c Check) { checks = append(checks, c) }

	// 1. Operating system.
	osCheck := Check{ID: "os_supported", Name: "Operating system supported", Status: CheckPass,
		Message: runtime.GOOS + "/" + runtime.GOARCH}
	if !platform.Supported() {
		osCheck.Status = CheckFail
		osCheck.Fix = "LocalDNS supports macOS, Linux and Windows."
	}
	add(osCheck)

	// 2-4. Hosts file found, readable, writable.
	path := m.HostsPath()
	found := Check{ID: "hosts_found", Name: "Hosts file found", Status: CheckPass, Message: path}
	readable := Check{ID: "hosts_readable", Name: "Hosts file readable", Status: CheckPass}
	writable := Check{ID: "hosts_writable", Name: "Hosts file writable", Status: CheckPass}
	section := Check{ID: "section_valid", Name: "LocalDNS section valid", Status: CheckPass}

	var raw []byte
	info, statErr := os.Stat(path)
	switch {
	case statErr != nil:
		found.Status = CheckFail
		found.Message = statErr.Error()
		found.Fix = "Your system should have a hosts file at " + platform.DefaultHostsFile() +
			". If you set " + platform.EnvHostsFile + " or --hosts-file, check the path."
	case info.IsDir():
		found.Status = CheckFail
		found.Message = path + " is a directory"
		found.Fix = "Point LocalDNS at the hosts file itself."
	}
	if found.Status == CheckPass {
		var err error
		raw, err = m.opts.Hosts.Read()
		if err != nil {
			readable.Status = CheckFail
			readable.Message = err.Error()
			readable.Fix = "Make sure " + path + " is readable by your user (it normally is)."
		}
		if err := m.opts.Hosts.CheckWritable(); err != nil {
			switch {
			case errors.Is(err, fs.ErrPermission) && m.opts.CanElevate:
				writable.Message = "requires administrator rights; LocalDNS asks for them (sudo) when needed"
			case errors.Is(err, fs.ErrPermission):
				writable.Status = CheckWarn
				writable.Message = "requires administrator rights"
				writable.Fix = m.opts.PermissionHint
			default:
				writable.Status = CheckFail
				writable.Message = err.Error()
				writable.Fix = "Check that " + path + " is not on a read-only filesystem and is not marked read-only."
			}
		}
	} else {
		readable = skipped(readable)
		writable = skipped(writable)
	}

	// 5. LocalDNS section.
	var doc *hosts.Document
	if raw != nil {
		doc = hosts.Parse(raw)
		if serr := doc.Err(); serr != nil {
			section.Status = CheckFail
			section.Message = serr.Error()
			section.Fix = AsError(sectionError(path, serr)).Hint
		} else if doc.HasSection() {
			section.Message = fmt.Sprintf("%d entries", len(doc.Entries()))
		} else {
			section.Message = "not created yet (it appears after the first `localdns add`)"
		}
	} else {
		section = skipped(section)
	}

	// 6. Configuration.
	cfgCheck := Check{ID: "config_valid", Name: "Configuration valid", Status: CheckPass, Message: m.ConfigPath()}
	cfg, err := m.loadConfig()
	switch {
	case err != nil:
		cfgCheck.Status = CheckFail
		cfgCheck.Message = err.Error()
		cfgCheck.Fix = AsError(err).Hint
	case !m.opts.Store.Exists():
		cfgCheck.Message = "not created yet (" + m.ConfigPath() + ")"
	case doc != nil && doc.Err() == nil:
		var problems []string
		for _, e := range entries(&state{raw: raw, doc: doc, cfg: cfg}) {
			if e.Status != StatusActive {
				problems = append(problems, e.Hostname+": "+e.Detail)
			}
		}
		if len(problems) > 0 {
			cfgCheck.Status = CheckWarn
			cfgCheck.Message = fmt.Sprintf("%d entries need attention", len(problems))
			cfgCheck.Fix = strings.Join(problems, "\n")
		}
	}

	add(found)
	add(readable)
	add(writable)
	add(section)
	add(cfgCheck)
	m.mu.Unlock()

	for _, f := range extra {
		add(f())
	}

	report := DoctorReport{OK: true, Checks: checks}
	for _, c := range checks {
		if c.Status == CheckFail {
			report.OK = false
		}
	}
	return report
}

func skipped(c Check) Check {
	c.Status = CheckFail
	c.Message = "skipped: hosts file not available"
	return c
}
