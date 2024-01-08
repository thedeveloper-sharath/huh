package huh

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/paginator"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Group is a collection of fields that are displayed together with a page of
// the form. While a group is displayed the form completer can switch between
// fields in the group.
//
// If any of the fields in a group have errors, the form will not be able to
// progress to the next group.
type Group struct {
	// collection of fields
	fields []Field

	// information
	title       string
	description string

	// navigation
	paginator paginator.Model
	viewport  viewport.Model

	// help
	showHelp bool
	help     help.Model

	// errors
	showErrors bool

	// group options
	width  int
	height int
	theme  *Theme
	keymap *KeyMap
	hide   func() bool
}

// NewGroup returns a new group with the given fields.
func NewGroup(fields ...Field) *Group {
	p := paginator.New()
	p.SetTotalPages(len(fields))

	group := &Group{
		fields:     fields,
		paginator:  p,
		help:       help.New(),
		showHelp:   true,
		showErrors: true,
	}

	height := group.fullHeight()
	v := viewport.New(80, height)
	group.viewport = v
	group.height = height

	return group
}

// Title sets the group's title.
func (g *Group) Title(title string) *Group {
	g.title = title
	return g
}

// Description sets the group's description.
func (g *Group) Description(description string) *Group {
	g.description = description
	return g
}

// WithShowHelp sets whether or not the group's help should be shown.
func (g *Group) WithShowHelp(show bool) *Group {
	g.showHelp = show
	return g
}

// WithShowErrors sets whether or not the group's errors should be shown.
func (g *Group) WithShowErrors(show bool) *Group {
	g.showErrors = show
	return g
}

// WithTheme sets the theme on a group.
func (g *Group) WithTheme(t *Theme) *Group {
	g.theme = t
	g.help.Styles = t.Help
	for _, field := range g.fields {
		field.WithTheme(t)
	}
	return g
}

// WithKeyMap sets the keymap on a group.
func (g *Group) WithKeyMap(k *KeyMap) *Group {
	g.keymap = k
	for _, field := range g.fields {
		field.WithKeyMap(k)
	}
	return g
}

// WithWidth sets the width on a group.
func (g *Group) WithWidth(width int) *Group {
	g.width = width
	g.viewport.Width = width
	for _, field := range g.fields {
		field.WithWidth(width)
	}
	return g
}

// WithHeight sets the height on a group.
func (g *Group) WithHeight(height int) *Group {
	g.height = height
	g.viewport.Height = height - 1
	for _, field := range g.fields {
		// A field height must not exceed the form height.
		if height-1 <= lipgloss.Height(field.View()) {
			field.WithHeight(height - 1)
		}
	}
	return g
}

// WithHide sets whether this group should be skipped.
func (g *Group) WithHide(hide bool) *Group {
	g.WithHideFunc(func() bool { return hide })
	return g
}

// WithHideFunc sets the function that checks if this group should be skipped.
func (g *Group) WithHideFunc(hideFunc func() bool) *Group {
	g.hide = hideFunc
	return g
}

// Errors returns the groups' fields' errors.
func (g *Group) Errors() []error {
	var errs []error
	for _, field := range g.fields {
		if err := field.Error(); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// nextFieldMsg is a message to move to the next field,
//
// each field controls when to send this message such that it is able to use
// different key bindings or events to trigger group progression.
type nextFieldMsg struct{}

// prevFieldMsg is a message to move to the previous field.
//
// each field controls when to send this message such that it is able to use
// different key bindings or events to trigger group progression.
type prevFieldMsg struct{}

// nextField is the command to move to the next field.
func nextField() tea.Msg {
	return nextFieldMsg{}
}

// prevField is the command to move to the previous field.
func prevField() tea.Msg {
	return prevFieldMsg{}
}

// Init initializes the group.
func (g *Group) Init() tea.Cmd {
	cmds := make([]tea.Cmd, len(g.fields)+1)
	for i, field := range g.fields {
		cmds[i] = field.Init()
	}
	cmd := g.fields[g.paginator.Page].Focus()
	cmds = append(cmds, cmd)
	return tea.Batch(cmds...)
}

// setCurrent sets the current field.
func (g *Group) setCurrent(current int) tea.Cmd {
	var (
		cmds []tea.Cmd
		cmd  tea.Cmd
	)

	cmd = g.fields[g.paginator.Page].Blur()
	cmds = append(cmds, cmd)

	g.paginator.Page = clamp(current, 0, len(g.fields)-1)

	cmd = g.fields[g.paginator.Page].Focus()
	cmds = append(cmds, cmd)

	return tea.Batch(cmds...)
}

// Update updates the group.
func (g *Group) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	m, cmd := g.fields[g.paginator.Page].Update(msg)
	g.fields[g.paginator.Page] = m.(Field)

	cmds = append(cmds, cmd)

	switch msg.(type) {
	case nextFieldMsg:
		current := g.paginator.Page
		cmd = g.setCurrent(current + 1)

		if current >= g.paginator.TotalPages-1 {
			cmds = append(cmds, nextGroup)
			break
		}

		offset := 0
		for i := 0; i <= current; i++ {
			offset += lipgloss.Height(g.fields[i].View()) + 1
		}
		g.viewport.SetYOffset(offset)

		cmds = append(cmds, cmd)

	case prevFieldMsg:
		current := g.paginator.Page
		cmd = g.setCurrent(current - 1)

		if current == 0 {
			cmds = append(cmds, prevGroup)
			break
		}

		offset := 0
		for i := 0; i < current-1; i++ {
			offset += lipgloss.Height(g.fields[i].View()) + 1
		}
		g.viewport.SetYOffset(offset)

		cmds = append(cmds, cmd)
	}

	return g, tea.Batch(cmds...)
}

// height returns the full height of the group
func (g *Group) fullHeight() int {
	var height int
	for _, f := range g.fields {
		height += lipgloss.Height(f.View()) + 1 // + gap
	}
	return height
}

// View renders the group.
func (g *Group) View() string {
	var fields strings.Builder
	var view strings.Builder

	gap := g.theme.FieldSeparator.String()
	if gap == "" {
		gap = "\n"
	}

	for i, field := range g.fields {
		fields.WriteString(field.View())
		if i < len(g.fields)-1 {
			fields.WriteString(gap)
		}
	}

	errors := g.Errors()
	g.viewport.SetContent(fields.String() + "\n")

	if g.showHelp && len(errors) <= 0 {
		// The short help view will be empty if (Field).KeyBinds() returns:
		//
		//   a. the nil or empty []key.Binding slice, or
		//   b. a []key.Binding slice with all elements disabled
		//
		// We don't want to render a spurious FieldSeparator gap in either case, but
		// case b. can only be determined by actually rendering the short help view.
		keys := g.help.ShortHelpView(g.fields[g.paginator.Page].KeyBinds())

		// (help.Model).ShortHelpView _will_ render an enabled key.Binding even when
		// its Key or Desc are undefined. If both are undefined, the binding is
		// rendered as a single space (" ").
		// Verify the rendered help view contains something other than whitespace.
		isNotSpace := func(r rune) bool { return !unicode.IsSpace(r) }
		if strings.IndexFunc(keys, isNotSpace) > 0 {
			view.WriteString(gap)
			view.WriteString(keys)
		}
	}

	if !g.showErrors {
		return g.viewport.View() + "\n" + view.String()
	}

	for _, err := range errors {
		view.WriteString(g.theme.Focused.ErrorMessage.Render(err.Error()))
		view.WriteRune('\n')
	}

	return g.viewport.View() + "\n" + view.String()
}
