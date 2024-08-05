package huh

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh/accessibility"
	"github.com/charmbracelet/lipgloss"
)

const (
	top int = iota
	bottom
	up
	down
	halfUp
	halfDown
)

const defaultLimit = 2

// MultiSelect is a form multi-select field.
type MultiSelect[T comparable] struct {
	accessor Accessor[[]T]
	key      string
	id       int

	// customization
	title           Eval[string]
	description     Eval[string]
	options         Eval[[]Option[T]]
	filterable      bool
	filteredOptions []Option[T]
	limit           int
	height          int

	// error handling
	validate func([]T) error
	err      error

	// state
	cursor    int
	focused   bool
	filtering bool
	filter    textinput.Model
	viewport  viewport.Model
	spinner   spinner.Model
	// avoid iterating over options to figure out what is selected.
	selected map[int]Option[T]

	// options
	width      int
	accessible bool
	theme      *Theme
	keymap     MultiSelectKeyMap
}

// NewMultiSelect returns a new multi-select field.
func NewMultiSelect[T comparable]() *MultiSelect[T] {
	filter := textinput.New()
	filter.Prompt = "/"

	s := spinner.New(spinner.WithSpinner(spinner.Line))

	return &MultiSelect[T]{
		accessor:    &EmbeddedAccessor[[]T]{},
		validate:    func([]T) error { return nil },
		filtering:   false,
		filter:      filter,
		id:          nextID(),
		options:     Eval[[]Option[T]]{cache: make(map[uint64][]Option[T])},
		title:       Eval[string]{cache: make(map[uint64]string)},
		description: Eval[string]{cache: make(map[uint64]string)},
		spinner:     s,
		selected:    make(map[int]Option[T]),
		limit:       defaultLimit,
	}
}

// Value sets the value of the multi-select field.
func (m *MultiSelect[T]) Value(value *[]T) *MultiSelect[T] {
	return m.Accessor(NewPointerAccessor(value))
}

// Accessor sets the accessor of the input field.
func (m *MultiSelect[T]) Accessor(accessor Accessor[[]T]) *MultiSelect[T] {
	m.accessor = accessor
	m.initSelectedValues(m.options.val...)
	return m
}

// Key sets the key of the select field which can be used to retrieve the value
// after submission.
func (m *MultiSelect[T]) Key(key string) *MultiSelect[T] {
	m.key = key
	return m
}

// Title sets the title of the multi-select field.
func (m *MultiSelect[T]) Title(title string) *MultiSelect[T] {
	m.title.val = title
	m.title.fn = nil
	return m
}

// TitleFunc sets the title func of the multi-select field.
func (m *MultiSelect[T]) TitleFunc(f func() string, bindings any) *MultiSelect[T] {
	m.title.fn = f
	m.title.bindings = bindings
	return m
}

// Description sets the description of the multi-select field.
func (m *MultiSelect[T]) Description(description string) *MultiSelect[T] {
	m.description.val = description
	return m
}

// DescriptionFunc sets the description func of the multi-select field.
func (m *MultiSelect[T]) DescriptionFunc(f func() string, bindings any) *MultiSelect[T] {
	m.description.fn = f
	m.description.bindings = bindings
	return m
}

// Options sets the options of the multi-select field.
func (m *MultiSelect[T]) Options(options ...Option[T]) *MultiSelect[T] {
	if len(options) <= 0 {
		return m
	}
	m.initSelectedValues(options...)
	m.options.val = options
	m.filteredOptions = options
	m.cursor = m.lowestSelectedIndex()
	m.updateViewportHeight()
	return m
}

// OptionsFunc sets the options func of the multi-select field.
func (m *MultiSelect[T]) OptionsFunc(f func() []Option[T], bindings any) *MultiSelect[T] {
	m.options.fn = f
	m.options.bindings = bindings
	m.filteredOptions = make([]Option[T], 0)
	// If there is no height set, we should attach a static height since these
	// options are possibly dynamic.
	if m.height <= 0 {
		m.height = defaultHeight
		m.updateViewportHeight()
	}
	m.cursor = m.lowestSelectedIndex()
	return m
}

// Filterable sets the multi-select field as filterable.
func (m *MultiSelect[T]) Filterable(filterable bool) *MultiSelect[T] {
	m.filterable = filterable
	return m
}

// Filtering sets the filtering state of the multi-select field.
func (m *MultiSelect[T]) Filtering(filtering bool) *MultiSelect[T] {
	m.filtering = filtering
	m.filter.Focus()
	return m
}

// Limit sets the limit of the multi-select field.
func (m *MultiSelect[T]) Limit(limit int) *MultiSelect[T] {
	m.limit = limit
	m.keymap.ToggleAll.SetEnabled(limit == 0)
	return m
}

// Height sets the height of the multi-select field.
func (m *MultiSelect[T]) Height(height int) *MultiSelect[T] {
	// What we really want to do is set the height of the viewport, but we
	// need a theme applied before we can calcualate its height.
	m.height = height
	m.updateViewportHeight()
	return m
}

// Validate sets the validation function of the multi-select field.
func (m *MultiSelect[T]) Validate(validate func([]T) error) *MultiSelect[T] {
	m.validate = validate
	return m
}

// Error returns the error of the multi-select field.
func (m *MultiSelect[T]) Error() error {
	return m.err
}

// Skip returns whether the multiselect should be skipped or should be blocking.
func (*MultiSelect[T]) Skip() bool {
	return false
}

// Zoom returns whether the multiselect should be zoomed.
func (*MultiSelect[T]) Zoom() bool {
	return false
}

// Focus focuses the multi-select field.
func (m *MultiSelect[T]) Focus() tea.Cmd {
	m.updateValue()
	m.focused = true
	return nil
}

// Blur blurs the multi-select field.
func (m *MultiSelect[T]) Blur() tea.Cmd {
	m.updateValue()
	m.focused = false
	return nil
}

// KeyBinds returns the help message for the multi-select field.
func (m *MultiSelect[T]) KeyBinds() []key.Binding {
	binds := []key.Binding{
		m.keymap.Toggle,
		m.keymap.Up,
		m.keymap.Down,
		m.keymap.Filter,
		m.keymap.SetFilter,
		m.keymap.ClearFilter,
		m.keymap.Prev,
		m.keymap.Submit,
		m.keymap.Next,
	}
	if m.limit == 0 {
		binds = append(binds, m.keymap.ToggleAll)
	}
	return binds
}

// Init initializes the multi-select field.
func (m *MultiSelect[T]) Init() tea.Cmd {
	return nil
}

// Update updates the multi-select field.
func (m *MultiSelect[T]) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Enforce height on the viewport during update as we need themes to
	// be applied before we can calculate the height.
	m.updateViewportHeight()

	var cmd tea.Cmd
	if m.filtering {
		m.filter, cmd = m.filter.Update(msg)
		cmds = append(cmds, cmd)
	}

	switch msg := msg.(type) {
	case updateFieldMsg:
		var cmds []tea.Cmd
		if ok, hash := m.title.shouldUpdate(); ok {
			m.title.bindingsHash = hash
			if !m.title.loadFromCache() {
				m.title.loading = true
				cmds = append(cmds, func() tea.Msg {
					return updateTitleMsg{id: m.id, title: m.title.fn(), hash: hash}
				})
			}
		}
		if ok, hash := m.description.shouldUpdate(); ok {
			m.description.bindingsHash = hash
			if !m.description.loadFromCache() {
				m.description.loading = true
				cmds = append(cmds, func() tea.Msg {
					return updateDescriptionMsg{id: m.id, description: m.description.fn(), hash: hash}
				})
			}
		}
		if ok, hash := m.options.shouldUpdate(); ok {
			m.options.bindingsHash = hash
			if m.options.loadFromCache() {
				m.filteredOptions = m.options.val
				m.updateValue()
				m.cursor = clamp(m.cursor, 0, len(m.filteredOptions)-1)
			} else {
				m.options.loading = true
				m.options.loadingStart = time.Now()
				cmds = append(cmds, func() tea.Msg {
					return updateOptionsMsg[T]{id: m.id, options: m.options.fn(), hash: hash}
				}, m.spinner.Tick)
			}
		}

		return m, tea.Batch(cmds...)

	case spinner.TickMsg:
		if !m.options.loading {
			break
		}
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case updateTitleMsg:
		if msg.id == m.id && msg.hash == m.title.bindingsHash {
			m.title.update(msg.title)
		}
	case updateDescriptionMsg:
		if msg.id == m.id && msg.hash == m.description.bindingsHash {
			m.description.update(msg.description)
		}
	case updateOptionsMsg[T]:
		if msg.id == m.id && msg.hash == m.options.bindingsHash {
			m.options.update(msg.options)
			// since we're updating the options, we need to reset the cursor.
			m.filteredOptions = m.options.val
			m.updateValue()
			m.cursor = clamp(m.cursor, 0, len(m.filteredOptions)-1)
		}
	case tea.KeyMsg:
		m.err = nil
		switch {
		case key.Matches(msg, m.keymap.Filter):
			m.setFilter(true)
			return m, m.filter.Focus()
		case key.Matches(msg, m.keymap.SetFilter):
			if len(m.filteredOptions) <= 0 {
				m.filter.SetValue("")
				m.filteredOptions = m.options.val
			}
			m.setFilter(false)
		case key.Matches(msg, m.keymap.ClearFilter):
			m.filter.SetValue("")
			m.filteredOptions = m.options.val
			m.setFilter(false)
		case key.Matches(msg, m.keymap.Up):
			if m.filtering && msg.String() == "k" {
				break
			}
			m.moveCursor(up)
		case key.Matches(msg, m.keymap.Down):
			if m.filtering && msg.String() == "j" {
				break
			}
			m.moveCursor(down)
		case key.Matches(msg, m.keymap.GotoTop):
			if m.filtering {
				break
			}
			m.moveCursor(top)
		case key.Matches(msg, m.keymap.GotoBottom):
			if m.filtering {
				break
			}
			m.moveCursor(bottom)
		case key.Matches(msg, m.keymap.HalfPageUp):
			m.moveCursor(halfUp)
		case key.Matches(msg, m.keymap.HalfPageDown):
			m.moveCursor(halfDown)
		case key.Matches(msg, m.keymap.Toggle) && !m.filtering:
			opt := m.options.val[m.cursor]
			m.ToggleSelect(m.cursor, opt)
			m.updateValue()
		case key.Matches(msg, m.keymap.ToggleAll) && m.limit == 0:
			selected := false

			for _, option := range m.filteredOptions {
				if !option.selected {
					selected = true
					break
				}
			}

			for i, option := range m.options.val {
				for j := range m.filteredOptions {
					if option.Key == m.filteredOptions[j].Key {
						m.options.val[i].selected = selected
						m.filteredOptions[j].selected = selected
						break
					}
				}
			}
			m.updateValue()
		case key.Matches(msg, m.keymap.Prev):
			m.updateValue()
			m.err = m.validate(m.accessor.Get())
			if m.err != nil {
				return m, nil
			}
			return m, PrevField
		case key.Matches(msg, m.keymap.Next, m.keymap.Submit):
			m.updateValue()
			m.err = m.validate(m.accessor.Get())
			if m.err != nil {
				return m, nil
			}
			return m, NextField
		}

		if m.filtering {
			m.filteredOptions = m.options.val
			if m.filter.Value() != "" {
				m.filteredOptions = nil
				for _, option := range m.options.val {
					if m.filterFunc(option.String()) {
						m.filteredOptions = append(m.filteredOptions, option)
					}
				}
			}
			if len(m.filteredOptions) > 0 {
				m.cursor = min(m.cursor, len(m.filteredOptions)-1)
			}
		}
	}

	return m, tea.Batch(cmds...)
}

// updateViewportHeight updates the viewport size according to the Height setting
// on this multi-select field.
func (m *MultiSelect[T]) updateViewportHeight() {
	// If no height is set size the viewport to the number of options.
	if m.height <= 0 {
		m.viewport.Height = len(m.options.val)
		return
	}

	const minHeight = 1
	m.viewport.Height = max(minHeight, m.height-
		lipgloss.Height(m.titleView())-
		lipgloss.Height(m.descriptionView()))
}

func (m *MultiSelect[T]) numSelected() int {
	return len(m.selected)
}

func (m *MultiSelect[T]) updateValue() {
	value := make([]T, 0)
	for i := range m.selected {
		value = append(value, m.selected[i].Value)
	}
	m.accessor.Set(value)
	m.err = m.validate(m.accessor.Get())
}

func (m *MultiSelect[T]) activeStyles() *FieldStyles {
	theme := m.theme
	if theme == nil {
		theme = ThemeCharm()
	}
	if m.focused {
		return &theme.Focused
	}
	return &theme.Blurred
}

func (m *MultiSelect[T]) titleView() string {
	if m.title.val == "" {
		return ""
	}
	var (
		styles = m.activeStyles()
		sb     = strings.Builder{}
	)
	if m.filtering {
		sb.WriteString(m.filter.View())
	} else if m.filter.Value() != "" {
		sb.WriteString(styles.Title.Render(m.title.val) + styles.Description.Render("/"+m.filter.Value()))
	} else {
		sb.WriteString(styles.Title.Render(m.title.val))
	}
	if m.err != nil {
		sb.WriteString(styles.ErrorIndicator.String())
	}
	return sb.String()
}

func (m *MultiSelect[T]) descriptionView() string {
	return m.activeStyles().Description.Render(m.description.val)
}

func (m *MultiSelect[T]) optionsView() string {
	var (
		styles = m.activeStyles()
		c      = styles.MultiSelectSelector.String()
		sb     strings.Builder
	)

	if m.options.loading && time.Since(m.options.loadingStart) > spinnerShowThreshold {
		m.spinner.Style = m.activeStyles().MultiSelectSelector.UnsetString()
		sb.WriteString(m.spinner.View() + " Loading...")
		return sb.String()
	}

	for i, option := range m.filteredOptions {
		if m.cursor == i {
			sb.WriteString(c)
		} else {
			sb.WriteString(strings.Repeat(" ", lipgloss.Width(c)))
		}

		if _, ok := m.selected[i]; ok {
			sb.WriteString(styles.SelectedPrefix.String())
			sb.WriteString(styles.SelectedOption.Render(option.String()))
		} else {
			sb.WriteString(styles.UnselectedPrefix.String())
			sb.WriteString(styles.UnselectedOption.Render(option.String()))
		}
		if i < len(m.options.val)-1 {
			sb.WriteString("\n")
		}
	}

	for i := len(m.filteredOptions); i < len(m.options.val)-1; i++ {
		sb.WriteString("\n")
	}

	return sb.String()
}

func (m *MultiSelect[T]) lowestSelectedIndex() int {
	if len(m.selected) == 0 {
		return 0
	}
	var indices []int
	for k := range m.selected {
		indices = append(indices, k)
	}
	return slices.Min(indices)
}

// startAtSelected makes the viewport content start at the selected element. Returns the index.
func (m *MultiSelect[T]) startAtSelected() int {
	if m.cursor > m.viewport.YOffset+m.viewport.Height {
		m.viewport.SetYOffset(m.cursor - m.viewport.YOffset)
	}
	return m.cursor
}

// View renders the multi-select field.
func (m *MultiSelect[T]) View() string {
	styles := m.activeStyles()
	m.viewport.SetContent(m.optionsView())
	m.startAtSelected()

	var sb strings.Builder
	if m.title.val != "" || m.title.fn != nil {
		sb.WriteString(m.titleView())
		sb.WriteString("\n")
	}
	if m.description.val != "" || m.description.fn != nil {
		sb.WriteString(m.descriptionView() + "\n")
	}
	sb.WriteString(m.viewport.View())
	return styles.Base.Render(sb.String())
}

func (m *MultiSelect[T]) printOptions() {
	styles := m.activeStyles()
	var sb strings.Builder
	sb.WriteString(styles.Title.Render(m.title.val))
	sb.WriteString("\n")

	for i, option := range m.options.val {
		if _, ok := m.selected[i]; ok {
			sb.WriteString(styles.SelectedOption.Render(fmt.Sprintf("%d. %s %s", i+1, "✓", option.String())))
		} else {
			sb.WriteString(fmt.Sprintf("%d. %s %s", i+1, " ", option.String()))
		}
		sb.WriteString("\n")
	}

	fmt.Println(sb.String())
}

// setFilter sets the filter of the select field.
func (m *MultiSelect[T]) setFilter(filter bool) {
	m.filtering = filter
	m.keymap.SetFilter.SetEnabled(filter)
	m.keymap.Filter.SetEnabled(!filter)
	m.keymap.Next.SetEnabled(!filter)
	m.keymap.Submit.SetEnabled(!filter)
	m.keymap.Prev.SetEnabled(!filter)
	m.keymap.ClearFilter.SetEnabled(!filter && m.filter.Value() != "")
}

// filterFunc returns true if the option matches the filter.
func (m *MultiSelect[T]) filterFunc(option string) bool {
	// XXX: remove diacritics or allow customization of filter function.
	return strings.Contains(strings.ToLower(option), strings.ToLower(m.filter.Value()))
}

// Run runs the multi-select field.
func (m *MultiSelect[T]) Run() error {
	if m.accessible {
		return m.runAccessible()
	}
	return Run(m)
}

// runAccessible() runs the multi-select field in accessible mode.
func (m *MultiSelect[T]) runAccessible() error {
	m.printOptions()
	styles := m.activeStyles()

	var choice int
	for {
		fmt.Printf("Select up to %d options. 0 to continue.\n", m.limit)

		choice = accessibility.PromptInt("Select: ", 0, len(m.options.val))
		if choice == 0 {
			m.updateValue()
			err := m.validate(m.accessor.Get())
			if err != nil {
				fmt.Println(err)
				continue
			}
			break
		}

		// Toggle Selection
		err := m.ToggleSelect(choice-1, m.options.val[choice-1])
		if err != nil {
			fmt.Printf("You can't select more than %d options.\n", m.limit)
			continue
		}

		// Provide confirmation message.
		if o, ok := m.selected[choice-1]; ok {
			// If it exists, it didn't before.
			fmt.Printf("Selected: %s\n\n", o.String())
		} else {
			fmt.Printf("Deselected: %s\n\n", o.String())
		}
		m.printOptions()
	}

	var values []string

	// TODO centralize this kind of loop
	value := m.accessor.Get()
	for i, option := range m.options.val {
		if _, ok := m.selected[i]; ok {
			value = append(value, option.Value)
			values = append(values, option.String())
			m.selected[i] = option
		}
	}
	m.accessor.Set(value)

	fmt.Println(styles.SelectedOption.Render("Selected:", strings.Join(values, ", ")+"\n"))
	return nil
}

// WithTheme sets the theme of the multi-select field.
func (m *MultiSelect[T]) WithTheme(theme *Theme) Field {
	if m.theme != nil {
		return m
	}
	m.theme = theme
	m.filter.Cursor.Style = theme.Focused.TextInput.Cursor
	m.filter.Cursor.TextStyle = theme.Focused.TextInput.CursorText
	m.filter.PromptStyle = theme.Focused.TextInput.Prompt
	m.filter.TextStyle = theme.Focused.TextInput.Text
	m.filter.PlaceholderStyle = theme.Focused.TextInput.Placeholder
	m.updateViewportHeight()
	return m
}

// WithKeyMap sets the keymap of the multi-select field.
func (m *MultiSelect[T]) WithKeyMap(k *KeyMap) Field {
	m.keymap = k.MultiSelect
	return m
}

// WithAccessible sets the accessible mode of the multi-select field.
func (m *MultiSelect[T]) WithAccessible(accessible bool) Field {
	m.accessible = accessible
	return m
}

// WithWidth sets the width of the multi-select field.
func (m *MultiSelect[T]) WithWidth(width int) Field {
	m.width = width
	return m
}

// WithHeight sets the total height of the multi-select field. Including padding
// and help menu heights.
func (m *MultiSelect[T]) WithHeight(height int) Field {
	m.Height(height)
	return m
}

// WithPosition sets the position of the multi-select field.
func (m *MultiSelect[T]) WithPosition(p FieldPosition) Field {
	if m.filtering {
		return m
	}
	m.keymap.Prev.SetEnabled(!p.IsFirst())
	m.keymap.Next.SetEnabled(!p.IsLast())
	m.keymap.Submit.SetEnabled(p.IsLast())
	return m
}

// GetKey returns the multi-select's key.
func (m *MultiSelect[T]) GetKey() string {
	return m.key
}

// GetValue returns the multi-select's value.
func (m *MultiSelect[T]) GetValue() any {
	return m.accessor.Get()
}

// ToggleSelect selects or deselects the option. Returns an error if the number
// of selected values exceeds the limit.
func (m *MultiSelect[T]) ToggleSelect(index int, o Option[T]) error {
	if _, ok := m.selected[index]; ok {
		delete(m.selected, index)
		return nil
	}
	if len(m.selected) >= m.limit {
		return errors.New("Limit reached. Unable to select another option.")
	}
	m.selected[index] = o
	return nil
}

// isSelected returns true if the value at the given index is selected.
func (m *MultiSelect[T]) isSelected(index int) bool {
	if _, ok := m.selected[index]; ok {
		return true
	}
	return false
}

// moveCursor repositions both the cursor and viewport offset while keeping
// values within bounds.
func (m *MultiSelect[T]) moveCursor(i int) {
	switch i {
	case top:
		m.cursor = top
		m.viewport.GotoTop()
	case up:
		m.cursor = max(m.cursor-1, 0)
		if m.cursor < m.viewport.YOffset {
			m.viewport.SetYOffset(m.cursor)
		}
	case down:
		m.cursor = min(m.cursor+1, len(m.filteredOptions)-1)
		if m.cursor >= m.viewport.YOffset+m.viewport.Height {
			m.viewport.LineDown(1)
		}
	case bottom:
		m.cursor = len(m.filteredOptions) - 1
		m.viewport.GotoBottom()
	case halfUp:
		m.cursor = max(m.cursor-m.viewport.Height/2, 0)
		m.viewport.HalfViewUp()
	case halfDown:
		m.cursor = min(m.cursor+m.viewport.Height/2, len(m.filteredOptions)-1)
		m.viewport.HalfViewDown()
	}
}

// initSelectedValues handles the Option's selected value that's set on
// instantiation and adds it to our map of selected items.
func (m *MultiSelect[T]) initSelectedValues(opts ...Option[T]) {
	for i, o := range opts {
		if o.selected {
			m.selected[i] = o
		}
	}
}
