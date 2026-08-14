package down

import (
	"context"
	"testing"
)

func TestNormalizeInputRemotePath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty is root", input: "", want: "/"},
		{name: "relative becomes absolute remote path", input: "Dir1/file.txt", want: "/Dir1/file.txt"},
		{name: "absolute stays absolute", input: "/Dir1/file.txt", want: "/Dir1/file.txt"},
		{name: "cleans path", input: "/Dir1/../Dir2//file.txt", want: "/Dir2/file.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeInputRemotePath(tt.input)
			if err != nil {
				t.Fatalf("normalizeInputRemotePath() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("normalizeInputRemotePath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRunRequiresPath(t *testing.T) {
	err := Runner{}.Run(context.Background(), Options{
		WorkingDir: ".",
	})
	if err == nil {
		t.Fatal("Run() error = nil, want error")
	}
}
