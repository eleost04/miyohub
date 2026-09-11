package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/store"
	"github.com/eleost04/miyohub/internal/tasks"
)

func inspectBBS(ctx context.Context, s *store.Store, o cliOptions, output io.Writer) error {
	accounts := []model.Account{}
	for _, a := range s.Config().Accounts {
		if _, runnable := s.AccountRunnable(a.ID); runnable && (o.account == "" || a.ID == o.account || a.Name == o.account) {
			accounts = append(accounts, a)
		}
	}
	if len(accounts) != 1 {
		return errors.New("请用 --account 指定唯一的已启用账号")
	}
	cfg, ok := s.ConfigForAccount(accounts[0].ID)
	if !ok {
		return errors.New("账号或所属用户已停用")
	}
	data, err := (tasks.BBSCheckin{Client: mihoyo.NewClient(""), Config: cfg, Account: accounts[0]}).InspectState(ctx)
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(data)
}

func showLogs(s *store.Store, o cliOptions, output io.Writer) error {
	entries := []model.LogEntry{}
	for _, entry := range s.RedactedLogsForUser("", true) {
		if o.logComponent == "" || entry.Component == o.logComponent {
			entries = append(entries, entry)
		}
	}
	if len(entries) > o.logTail {
		entries = entries[len(entries)-o.logTail:]
	}
	if o.json {
		return json.NewEncoder(output).Encode(entries)
	}
	zone, err := time.LoadLocation(s.Config().Schedule.Timezone)
	if err != nil {
		zone = time.UTC
	}
	for _, entry := range entries {
		if _, err := fmt.Fprintf(output, "%s [%s] %s\n", entry.At.In(zone).Format(time.DateTime), entry.Component, entry.Message); err != nil {
			return err
		}
	}
	return nil
}
