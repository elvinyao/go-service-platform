package health

import "context"

func callReporter(ctx context.Context, reporter Reporter, report *Report) (recovered interface{}) {
	defer func() {
		recovered = recover()
	}()
	reporter.ReportHealth(nonNilContext(ctx), report)
	return nil
}
