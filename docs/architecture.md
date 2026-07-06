# Brain Architecture

## Model Roles

Brain uses separate model roles for embedding and attachment extraction. These roles serve different parts of the content pipeline and should remain independently configurable.

### Embedding Models

The embedding role converts text into vectors for semantic retrieval. It operates on text that Brain already understands, such as note bodies, task content, summaries, and derived text from attachments.

Embedding models are optimized for similarity search rather than content interpretation. Their output is stored as vector data in the embedding index and used by semantic and hybrid search to find entries by meaning.

### Attachment Extraction Models

The attachment extraction role converts media into text. It handles files such as images, PDFs, audio, and other attachment formats that are not directly searchable as plain Markdown.

Extraction models are optimized for understanding source media and producing readable derived text. They do not write vectors directly. Their output becomes normal text content that Brain can inspect, index, and pass to the embedding pipeline.

### Derived Text Bridge

Derived attachment text is the bridge between media storage and Brain search. The attachment blob remains stored as the source artifact, while extracted text is saved as derived metadata associated with that attachment.

Search then works in two layers:

- Full-text search can match the derived text directly.
- Embedding backfill can convert the derived text into vectors for semantic and hybrid search.

This keeps media interpretation separate from retrieval. Attachment extraction answers "what text can we derive from this file?" Embedding answers "how should this text be represented for semantic search?"

### Production-Safe Defaults

Attachment extraction is disabled by default. This prevents new deployments from unexpectedly sending uploaded files to a model provider, incurring model costs, increasing latency, or processing sensitive media without an explicit operator decision.

Embeddings are also optional and fall back safely to full-text search when unavailable. Together, these defaults keep Brain useful in local and production environments without requiring external model services.

Operators should enable attachment extraction only after choosing an appropriate provider, model, timeout, and data-handling policy for their deployment. Once enabled, extracted text flows through the same search and embedding paths as other Brain text.
