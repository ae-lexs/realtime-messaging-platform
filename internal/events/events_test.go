package events_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/aelexs/realtime-messaging-platform/internal/events"
)

// repoRoot is where the .proto paths in Sources resolve from; tests run in the
// package directory.
const repoRoot = "../.."

// TestEachSchemaHoldsOneMessage guards the layout the registry enforces: a
// schema with more than one top-level message is rejected with "Too many
// message types specified in schema definition", and one message per schema is
// also what fixes every wire message index at 0. Adding a second message to any
// events/v1 file would pass `buf lint` and fail at publish time.
func TestEachSchemaHoldsOneMessage(t *testing.T) {
	for _, source := range events.Sources() {
		t.Run(source.Subject, func(t *testing.T) {
			// Arrange
			text, err := os.ReadFile(filepath.Join(repoRoot, source.File))
			require.NoError(t, err)

			// Act
			messages := strings.Count(string(text), "\nmessage ")

			// Assert
			assert.Equal(t, 1, messages, "%s must declare exactly one top-level message", source.File)
		})
	}
}

// TestSourcesAreOrderedByDependency pins the registration order: the registry
// resolves no imports itself, so a schema must be registered before anything
// that references it.
func TestSourcesAreOrderedByDependency(t *testing.T) {
	// Arrange
	registered := map[string]bool{}

	for _, source := range events.Sources() {
		t.Run(source.Subject, func(t *testing.T) {
			// Assert — every import is already registered...
			for _, imported := range source.Imports {
				assert.True(t, registered[imported], "%s imports %s before it is registered", source.File, imported)
			}

			// ...and the file declares exactly the imports listed.
			text, err := os.ReadFile(filepath.Join(repoRoot, source.File))
			require.NoError(t, err)
			assert.Equal(t, strings.Count(string(text), "\nimport "), len(source.Imports),
				"%s declares imports the source table does not list", source.File)

			registered[source.Path] = true
		})
	}
}

// TestEventSubjectsHaveASchemaSource keeps the two tables in step: every topic
// the serde can encode must have a schema someone publishes.
func TestEventSubjectsHaveASchemaSource(t *testing.T) {
	// Arrange
	published := map[string]bool{}
	for _, source := range events.Sources() {
		published[source.Subject] = true
	}

	// Assert
	for _, subject := range events.Subjects() {
		assert.True(t, published[subject], "%s has no schema source", subject)
	}
}

// TestGeneratedTypesMatchTheSchemaFiles ties the Go types back to the .proto
// files that get registered — a type generated from a file nobody publishes
// would encode against a schema ID that does not exist.
func TestGeneratedTypesMatchTheSchemaFiles(t *testing.T) {
	for _, d := range events.Descriptors() {
		t.Run(d.Topic, func(t *testing.T) {
			// Arrange
			descriptor := d.New().ProtoReflect().Descriptor()

			// Act
			file, err := protoregistry.GlobalFiles.FindFileByPath(descriptor.ParentFile().Path())

			// Assert
			require.NoError(t, err)
			assert.Equal(t, 1, file.Messages().Len(), "%s must be the only message in %s", descriptor.Name(), file.Path())

			var source *events.SchemaSource
			for _, s := range events.Sources() {
				if s.Subject == d.Subject() {
					source = &s
					break
				}
			}
			require.NotNil(t, source, "no schema source for %s", d.Subject())
			assert.Equal(t, source.Path, file.Path(), "the registered file and the generated type must come from the same .proto")
		})
	}
}

func TestSubjectsUseTopicNameStrategy(t *testing.T) {
	// Act
	subjects := events.Subjects()

	// Assert
	assert.Equal(t, []string{
		"messages.persisted-value",
		"memberships.changed-value",
		"chats.created-value",
	}, subjects)
}

func TestDescriptorForTopicUnknown(t *testing.T) {
	// Act
	_, err := events.DescriptorForTopic("messages.deleted")

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "messages.deleted")
}
