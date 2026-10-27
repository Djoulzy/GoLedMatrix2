package main

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/Djoulzy/GoLedMatrix2/internal/client"
)

func scanServers(ctx context.Context, output io.Writer, options client.ScanOptions) error {
	results, scanErr := client.Scan(ctx, options)
	if len(results) == 0 {
		if scanErr == nil {
			_, err := fmt.Fprintln(output, "no GoLedMatrix2 servers found")
			return err
		}
		return scanErr
	}
	table := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	fmt.Fprintln(table, "URL\tMATRIX\tBACKEND")
	for _, result := range results {
		fmt.Fprintf(table, "%s\t%dx%d\t%s\n", result.URL, result.Info.Width, result.Info.Height, result.Info.Backend)
	}
	if err := table.Flush(); err != nil {
		return err
	}
	return scanErr
}
