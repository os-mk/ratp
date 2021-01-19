package ratp

type nilLogger struct{}

func (nilLogger) Print(...interface{})          {}
func (nilLogger) Printf(string, ...interface{}) {}
func (nilLogger) Println(...interface{})        {}
