/**
 * Faces — enrolled face identity management page (Phase 13, R11).
 * Lists enrolled faces with name and creation date.
 * Admins can enroll a new face via JPEG photo upload (calls sentinel-infer) or
 * via the raw embedding API (for programmatic use). Delete is also admin-only.
 */
import { useEffect, useState, useRef } from "react";
import { Users, Trash2, UserPlus, Upload, ChevronDown, ChevronUp } from "lucide-react";
import { api, FaceRecord } from "../api/client";
import { useAuth } from "../context/AuthContext";
import Toast from "../components/Toast";
import { useToast } from "../hooks/useToast";

export default function Faces() {
  const { user } = useAuth();
  const isAdmin = user?.role === "admin";
  const { toast, showToast, dismissToast } = useToast();
  const [faces, setFaces] = useState<FaceRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Photo enrollment state
  const [enrollName, setEnrollName] = useState("");
  const [enrollFile, setEnrollFile] = useState<File | null>(null);
  const [enrollLoading, setEnrollLoading] = useState(false);
  const [enrollError, setEnrollError] = useState<string | null>(null);
  const [enrollSuccess, setEnrollSuccess] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const enrollTimerRef = useRef<ReturnType<typeof setTimeout>>(null);

  // Raw embedding panel (advanced)
  const [showRawPanel, setShowRawPanel] = useState(false);
  const [rawName, setRawName] = useState("");
  const [rawEmbedding, setRawEmbedding] = useState("");
  const [rawLoading, setRawLoading] = useState(false);
  const [rawError, setRawError] = useState<string | null>(null);

  const enrollCtrlRef = useRef<AbortController | null>(null);
  const manualCtrlRef = useRef<AbortController | null>(null);

  useEffect(() => {
    const ctrl = new AbortController();
    api
      .listFaces(ctrl.signal)
      .then((data) => {
        setFaces(data);
        setLoading(false);
      })
      .catch((err) => {
        if (err instanceof DOMException && err.name === "AbortError") return;
        setError(err.message);
        setLoading(false);
      });
    return () => ctrl.abort();
  }, []);

  useEffect(() => () => {
    enrollCtrlRef.current?.abort();
    manualCtrlRef.current?.abort();
    if (enrollTimerRef.current) clearTimeout(enrollTimerRef.current);
  }, []);

  async function handlePhotoEnroll(e: React.FormEvent) {
    e.preventDefault();
    if (!enrollFile || !enrollName.trim() || enrollLoading) return;
    setEnrollLoading(true);
    setEnrollError(null);
    setEnrollSuccess(false);

    enrollCtrlRef.current?.abort();
    const ctrl = new AbortController();
    enrollCtrlRef.current = ctrl;

    try {
      const face = await api.enrollFaceFromPhoto(enrollName.trim(), enrollFile, ctrl.signal);
      if (ctrl.signal.aborted) return;
      setFaces((prev) => [...prev, face]);
      setEnrollName("");
      setEnrollFile(null);
      if (fileInputRef.current) fileInputRef.current.value = "";
      setEnrollSuccess(true);
      showToast("Face enrolled successfully", "success");
      enrollTimerRef.current = setTimeout(() => setEnrollSuccess(false), 3_000);
    } catch (err) {
      if (ctrl.signal.aborted) return;
      setEnrollError(err instanceof Error ? err.message : "Enrollment failed");
    } finally {
      setEnrollLoading(false);
    }
  }

  async function handleRawEnroll(e: React.FormEvent) {
    e.preventDefault();
    if (rawLoading) return;
    setRawLoading(true);
    setRawError(null);

    let embedding: number[];
    try {
      embedding = JSON.parse(rawEmbedding);
      if (!Array.isArray(embedding) || embedding.length !== 512) {
        throw new Error("Embedding must be a JSON array of exactly 512 numbers");
      }
    } catch (err) {
      setRawError(err instanceof Error ? err.message : "Invalid JSON");
      setRawLoading(false);
      return;
    }

    try {
      const face = await api.createFace({ name: rawName.trim(), embedding });
      setFaces((prev) => [...prev, face]);
      setRawName("");
      setRawEmbedding("");
      showToast("Face enrolled via embedding", "success");
    } catch (err) {
      setRawError(err instanceof Error ? err.message : "Failed to enroll face");
    } finally {
      setRawLoading(false);
    }
  }

  async function handleDelete(id: number, name: string) {
    if (!isAdmin) return;
    if (!window.confirm(`Delete enrolled face "${name}"? This cannot be undone.`)) return;
    manualCtrlRef.current?.abort();
    const ctrl = new AbortController();
    manualCtrlRef.current = ctrl;
    try {
      await api.deleteFace(id, ctrl.signal);
      if (ctrl.signal.aborted) return;
      setFaces((prev) => prev.filter((f) => f.id !== id));
      showToast("Face deleted", "success");
    } catch (err) {
      if (ctrl.signal.aborted) return;
      const msg = err instanceof Error ? err.message : "Delete failed";
      setError(msg);
      showToast(msg, "error");
    }
  }

  return (
    <div className="p-8 flex flex-col gap-6 min-h-full">
      {/* Header */}
      <div className="flex items-center gap-3">
        <Users className="w-6 h-6 text-purple-400" />
        <h1 className="text-2xl font-semibold">Enrolled Faces</h1>
        <span className="text-sm text-muted ml-auto">{faces.length} enrolled</span>
      </div>

      {error && (
        <div className="bg-red-900/20 border border-red-800 rounded-lg p-4">
          <p className="text-red-400 text-sm">{error}</p>
        </div>
      )}

      {/* Enrollment section (admin only) */}
      {isAdmin && (
        <div className="space-y-3">
          {/* Photo upload enrollment */}
          <section className="bg-surface-raised border border-border rounded-lg p-5">
            <h2 className="text-sm font-medium text-muted mb-3 flex items-center gap-2">
              <Upload className="w-4 h-4" />
              Enroll from Photo
            </h2>
            <p className="text-xs text-faint mb-4">
              Upload a clear JPEG of the person's face. Requires face recognition to be enabled
              in the server configuration (detection.face_recognition.enabled).
            </p>

            {enrollSuccess && (
              <p className="text-green-400 text-sm mb-3">Face enrolled successfully.</p>
            )}
            {enrollError && (
              <p className="text-red-400 text-sm mb-3">{enrollError}</p>
            )}

            <form onSubmit={handlePhotoEnroll} className="space-y-3">
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                <div>
                  <label htmlFor="face-enroll-name" className="block text-xs text-muted mb-1">Name</label>
                  <input
                    id="face-enroll-name"
                    type="text"
                    value={enrollName}
                    onChange={(e) => setEnrollName(e.target.value)}
                    placeholder="e.g. Alice Smith"
                    maxLength={128}
                    className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white placeholder-faint focus:outline-none focus:border-sentinel-500"
                  />
                </div>
                <div>
                  <label htmlFor="face-photo-upload" className="block text-xs text-muted mb-1">Photo (JPEG, max 16 MB)</label>
                  <input
                    id="face-photo-upload"
                    ref={fileInputRef}
                    type="file"
                    accept="image/jpeg,image/jpg"
                    onChange={(e) => setEnrollFile(e.target.files?.[0] ?? null)}
                    className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white file:mr-3 file:bg-sentinel-500 file:text-white file:border-0 file:rounded file:px-2 file:py-1 file:text-xs file:cursor-pointer focus:outline-none"
                  />
                </div>
              </div>
              <div className="flex justify-end">
                <button
                  type="submit"
                  disabled={!enrollName.trim() || !enrollFile || enrollLoading}
                  className="bg-sentinel-500 hover:bg-sentinel-600 disabled:opacity-50 text-white px-4 py-2 rounded-lg text-sm font-medium transition-colors flex items-center gap-2"
                >
                  <UserPlus className="w-4 h-4" />
                  {enrollLoading ? "Enrolling…" : "Enroll Face"}
                </button>
              </div>
            </form>
          </section>

          {/* Raw embedding API (collapsed by default) */}
          <section className="bg-surface-raised border border-border rounded-lg p-5">
            <button
              type="button"
              onClick={() => setShowRawPanel((v) => !v)}
              className="w-full flex items-center justify-between text-sm font-medium text-muted"
            >
              <span>Raw Embedding API</span>
              {showRawPanel ? <ChevronUp className="w-4 h-4" /> : <ChevronDown className="w-4 h-4" />}
            </button>

            {showRawPanel && (
              <div className="mt-4">
                <p className="text-xs text-faint mb-4">
                  Advanced: supply a pre-computed 512-dim ArcFace embedding as a JSON float array.
                  Useful for programmatic enrollment (POST /api/v1/faces).
                </p>

                {rawError && (
                  <p className="text-red-400 text-sm mb-3">{rawError}</p>
                )}

                <form onSubmit={handleRawEnroll} className="space-y-3">
                  <div>
                    <label className="block text-xs text-muted mb-1">Name</label>
                    <input
                      type="text"
                      value={rawName}
                      onChange={(e) => setRawName(e.target.value)}
                      placeholder="Person name"
                      maxLength={128}
                      className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white placeholder-faint focus:outline-none focus:border-sentinel-500"
                    />
                  </div>
                  <div>
                    <label className="block text-xs text-muted mb-1">
                      Embedding (JSON array, 512 float32 values)
                    </label>
                    <textarea
                      value={rawEmbedding}
                      onChange={(e) => setRawEmbedding(e.target.value)}
                      placeholder="[0.123, -0.456, ...]"
                      rows={3}
                      className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white font-mono placeholder-faint focus:outline-none focus:border-sentinel-500 resize-y"
                    />
                  </div>
                  <div className="flex justify-end">
                    <button
                      type="submit"
                      disabled={!rawName.trim() || !rawEmbedding.trim() || rawLoading}
                      className="bg-surface-base hover:bg-surface-raised border border-border disabled:opacity-50 text-white px-4 py-2 rounded-lg text-sm font-medium transition-colors"
                    >
                      {rawLoading ? "Enrolling…" : "Enroll via Embedding"}
                    </button>
                  </div>
                </form>
              </div>
            )}
          </section>
        </div>
      )}

      {/* Face grid */}
      {loading ? (
        <div className="flex-1 flex items-center justify-center text-muted">
          Loading faces...
        </div>
      ) : faces.length === 0 ? (
        <div className="flex-1 flex flex-col items-center justify-center gap-3">
          <UserPlus className="w-12 h-12 text-faint" />
          <p className="text-muted">No faces enrolled</p>
          {isAdmin ? (
            <p className="text-sm text-faint">
              Use the enrollment form above to add a face from a photo.
            </p>
          ) : (
            <p className="text-sm text-faint max-w-md text-center">
              Ask an admin to enroll faces via the Faces page or POST /api/v1/faces.
            </p>
          )}
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
          {faces.map((face) => (
            <FaceCard
              key={face.id}
              face={face}
              isAdmin={isAdmin}
              onDelete={handleDelete}
            />
          ))}
        </div>
      )}
      {toast && <Toast message={toast.message} type={toast.type} onDismiss={dismissToast} />}
    </div>
  );
}

function FaceCard({
  face,
  isAdmin,
  onDelete,
}: {
  face: FaceRecord;
  isAdmin: boolean;
  onDelete: (id: number, name: string) => void;
}) {
  const createdDate = new Date(face.created_at).toLocaleDateString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
  });

  return (
    <div className="bg-surface-raised border border-border rounded-lg p-5 flex items-center gap-4">
      {/* Avatar placeholder */}
      <div className="w-12 h-12 rounded-full bg-purple-500/20 flex items-center justify-center shrink-0">
        <Users className="w-6 h-6 text-purple-400" />
      </div>
      <div className="flex-1 min-w-0">
        <h3 className="font-medium truncate">{face.name}</h3>
        <p className="text-xs text-faint">Enrolled {createdDate}</p>
      </div>
      {isAdmin && (
        <button
          onClick={() => onDelete(face.id, face.name)}
          className="p-1.5 text-faint hover:text-red-400 transition-colors shrink-0"
          aria-label={`Delete enrolled face ${face.name}`}
          title={`Delete ${face.name}`}
        >
          <Trash2 className="w-4 h-4" />
        </button>
      )}
    </div>
  );
}
