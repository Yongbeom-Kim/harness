package reviewloop

import "fmt"

type SessionNameBuilder struct{}

func NewSessionNameBuilder() SessionNameBuilder {
	return SessionNameBuilder{}
}

func (SessionNameBuilder) Build(runID string, role string) string {
	return fmt.Sprintf("iwr-%s-%s", runID, role)
}
