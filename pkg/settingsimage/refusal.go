// SPDX-License-Identifier: Apache-2.0

package settingsimage

import "fmt"

// RefusalError is a folder that breaks a rule of format-v2.md §8. It is permanent: building the
// same input again cannot succeed. Match it with errors.As.
type RefusalError struct {
	// Path is the offending path, slash-separated and relative to the folder. It is "." when the
	// refusal is about the folder as a whole.
	Path string
	// Reason says which rule was broken.
	Reason string
}

func (e *RefusalError) Error() string {
	return fmt.Sprintf("refused %s: %s", e.Path, e.Reason)
}

func refuse(path, format string, args ...any) *RefusalError {
	return &RefusalError{Path: path, Reason: fmt.Sprintf(format, args...)}
}
