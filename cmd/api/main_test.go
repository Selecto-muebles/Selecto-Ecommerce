package main

import "testing"

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		job     string
		jobMode bool
		wantErr bool
	}{
		{name: "api", args: nil},
		{name: "expire", args: []string{"job", "expire-orders"}, job: "expire-orders", jobMode: true},
		{name: "email", args: []string{"job", "email-outbox"}, job: "email-outbox", jobMode: true},
		{name: "unknown", args: []string{"job", "unknown"}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			job, jobMode, err := parseCommand(test.args)
			if (err != nil) != test.wantErr || job != test.job || jobMode != test.jobMode {
				t.Fatalf("parseCommand() = (%q, %v, %v)", job, jobMode, err)
			}
		})
	}
}
