# Testing Rules

## Unit Tests
- Test all public functions and methods
- Use `testing/quick` for property-based testing where appropriate
- Mock external dependencies using interfaces
- Each test must be reproducible (no shared state between tests)

## Integration Tests
- Use `testcontainers-go` for database integration tests
- Tag integration tests with `//go:build integration`
- Run integration tests separately from unit tests
- Clean up test resources after each test

## Test Coverage
- Minimum 80% line coverage
- Cover error paths, not just happy paths
- Use coverage reports to identify untested code
- Add tests for regressions when fixing bugs

## Table-Driven Tests
```go
func TestSomething(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    string
        wantErr bool
    }{
        {name: "valid input", input: "hello", want: "HELLO", wantErr: false},
        {name: "empty input", input: "", want: "", wantErr: true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := SomeFunc(tt.input)
            if tt.wantErr {
                require.Error(t, err)
                return
            }
            require.NoError(t, err)
            assert.Equal(t, tt.want, got)
        })
    }
}
```
