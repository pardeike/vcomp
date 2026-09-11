package tui

import (
	"cmp"
	"fmt"
	"sort"
	"strings"
	"vcomp/internal/config"
)

func sortKey(screen int) string {
	switch screen {
	case 0:
		return "dashboard_sort"
	case 1:
		return "public_tests_sort"
	case 6:
		return "catalogue_sort"
	}
	return ""
}
func (a *app) sortAction(act action) error {
	key := sortKey(a.m.Screen)
	if key == "" {
		return fmt.Errorf("this screen has no table sort")
	}
	if act.Kind == "sort-save" {
		value := act.Values[0]
		if act.Values[1] == "descending" {
			value = "-" + value
		}
		if err := config.UpdateLocal(a.m.Root, []config.Override{{Key: key, Value: value}}); err != nil {
			return err
		}
		a.m.Form = nil
		a.m.Message = "Sort saved for this company."
		a.reload()
		return nil
	}
	value := a.m.Data.View.Config.UISort[key]
	direction := "ascending"
	if strings.HasPrefix(value, "-") {
		direction = "descending"
	}
	a.m.Form = newForm("sort-save", "Sort "+screens[a.m.Screen], []field{
		{Label: "Sort by", Value: strings.TrimPrefix(value, "-"), Kind: kindChoice, Options: valueOptions(config.SortFields(key), "")},
		{Label: "Direction", Value: direction, Kind: kindChoice, Options: valueOptions([]string{"ascending", "descending"}, "")},
	}, nil)
	return nil
}
func sortData(d *data) {
	order := func(key string) (string, bool) {
		value := d.View.Config.UISort[key]
		return strings.TrimPrefix(value, "-"), strings.HasPrefix(value, "-")
	}
	less := func(comparison int, desc bool, left, right string) bool {
		if comparison == 0 {
			return left < right
		}
		if desc {
			return comparison > 0
		}
		return comparison < 0
	}
	field, desc := order("dashboard_sort")
	sort.SliceStable(d.View.Agents, func(i, j int) bool {
		a, b := d.View.Agents[i], d.View.Agents[j]
		c := 0
		switch field {
		case "state":
			c = cmp.Compare(a.State, b.State)
		case "inbox":
			c = cmp.Compare(a.Inbox, b.Inbox)
		case "turns":
			c = cmp.Compare(a.Turns.Completed, b.Turns.Completed)
		case "started":
			c = a.Turns.Started.Compare(b.Turns.Started)
		case "average":
			c = cmp.Compare(a.Turns.Average, b.Turns.Average)
		case "harness":
			c = cmp.Compare(a.Harness, b.Harness)
		default:
			c = cmp.Compare(a.Name, b.Name)
		}
		return less(c, desc, a.Name, b.Name)
	})
	field, desc = order("catalogue_sort")
	sort.SliceStable(d.Catalogue, func(i, j int) bool {
		a, b := d.Catalogue[i], d.Catalogue[j]
		c := 0
		switch field {
		case "title":
			c = cmp.Compare(a.Title, b.Title)
		case "sector":
			c = cmp.Compare(a.Sector, b.Sector)
		case "state":
			if a.Deleted != b.Deleted {
				if a.Deleted {
					c = 1
				} else {
					c = -1
				}
			}
		default:
			c = cmp.Compare(a.Name, b.Name)
		}
		return less(c, desc, a.Name, b.Name)
	})
	field, desc = order("public_tests_sort")
	sort.SliceStable(d.View.Runs, func(i, j int) bool {
		a, b := d.View.Runs[i], d.View.Runs[j]
		c := 0
		switch field {
		case "created":
			c = a.Created.Compare(b.Created)
		case "state":
			c = cmp.Compare(a.State, b.State)
		case "attempts":
			c = cmp.Compare(a.Attempts, b.Attempts)
		default:
			c = cmp.Compare(a.Name, b.Name)
		}
		return less(c, desc, a.Name, b.Name)
	})
}
