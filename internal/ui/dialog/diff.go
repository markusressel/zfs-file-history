package dialog

import (
	"os"
	"os/exec"
	"strings"
)

const (
	DiffBinPath = "/usr/bin/diff"
)

// DiffBinExists checks if the diff binary is available on the system.
func DiffBinExists() bool {
	_, err := exec.LookPath(DiffBinPath)
	return err == nil
}

// RunDiff executes the system diff command on two files and returns the output.
func RunDiff(oldPath, newPath string) (string, error) {
	output, err := exec.Command(
		DiffBinPath,
		"-U", "3",
		oldPath,
		newPath,
	).Output()
	if err != nil && err.Error() != "exit status 1" {
		return "", err
	}
	return string(output), nil
}

// IsBinaryFile checks if a file contains null bytes, indicating it is binary.
func IsBinaryFile(path string) bool {
	if path == DevNull {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	for i := 0; i < n; i++ {
		if buf[i] == 0 {
			return true
		}
	}
	return false
}

// FormatDiffText highlights diff additions (green) and deletions (red) with tview color tags.
// If filterHeaders is true, it removes diff unified header lines (--- and +++).
func FormatDiffText(diffText string, filterHeaders bool) string {
	diffTextLines := strings.Split(diffText, "\n")
	var resultLines []string
	for _, line := range diffTextLines {
		if filterHeaders {
			if len(line) >= 4 && (strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++")) && (line[3] == ' ' || line[3] == '\t') {
				continue
			}
		}
		if strings.HasPrefix(line, "+") {
			resultLines = append(resultLines, `[green]`+line+`[white]`)
		} else if strings.HasPrefix(line, "-") {
			resultLines = append(resultLines, `[red]`+line+`[white]`)
		} else {
			resultLines = append(resultLines, line)
		}
	}
	return strings.Join(resultLines, "\n")
}
