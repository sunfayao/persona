package saved

import "context"

// Prompts is the saved prompts for the assistant's own tools (persona_settings): the same checks and the same store as
// the routes, so a prompt the model adds is one the "/" list shows.
type Prompts struct{ b *promptBook }

// NewPrompts is the prompts of host h.
func NewPrompts(h Host) *Prompts { return &Prompts{b: &promptBook{h: h}} }

// List is the prompts in the "/" list's order.
func (p *Prompts) List(ctx context.Context) ([]Prompt, error) { return p.b.prompts(ctx) }

// Check says whether name and text make a prompt, and the prompt they make (name and text as they would be saved).
func Check(name, text string) (Prompt, error) {
	return savedPromptInput{Name: name, Text: text}.prompt()
}

// Add saves a prompt at the end of the list.
func (p *Prompts) Add(ctx context.Context, name, text string) (Prompt, error) {
	in, err := Check(name, text)
	if err != nil {
		return Prompt{}, err
	}
	return p.b.add(ctx, in)
}

// Update changes prompt id's name and text.
func (p *Prompts) Update(ctx context.Context, id, name, text string) (Prompt, error) {
	in, err := Check(name, text)
	if err != nil {
		return Prompt{}, err
	}
	return p.b.update(ctx, id, in)
}

// Delete removes prompt id.
func (p *Prompts) Delete(ctx context.Context, id string) error { return p.b.delete(ctx, id) }
