package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/Djoulzy/GoLedMatrix2/internal/client"
)

func serverFromScan(results []client.ScanResult, scanErr error) (string, error) {
	if scanErr != nil {
		return "", fmt.Errorf("discovery incomplete: %w; provide -server to choose a target", scanErr)
	}
	if len(results) == 0 {
		return "", fmt.Errorf("no GoLedMatrix2 servers found; provide -server or retry discovery")
	}
	if len(results) > 1 {
		var urls []string
		for _, result := range results {
			urls = append(urls, result.URL)
		}
		return "", fmt.Errorf("multiple server addresses found; provide -server to choose one:\n%s", strings.Join(urls, "\n"))
	}
	return results[0].URL, nil
}

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
