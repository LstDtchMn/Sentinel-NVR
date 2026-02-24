/**
 * Faces — enrolled face identity management page (Phase 13, R11).
 * Lists enrolled faces with name and creation date.
 * Admin users can delete faces. Enrollment is done via the API
 * (POST /api/v1/faces with pre-computed embedding) until the sentinel-infer
 * face embedding endpoint is fully implemented.
 */
import { useEffect, useState, useRef } from "react";
import { Users, Trash2, UserPlus } from "lucide-react";
import { api, FaceRecord } from "../api/client";
import { useAuth } from "../context/AuthContext";

export default function Faces() {
  const { user } = useAuth();
  const isAdmin = user?.role === "admin";
  const [faces, setFaces] = useState<FaceRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
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

  // Abort manual controllers on unmount.
  useEffect(() => () => manualCtrlRef.current?.abort(), []);

  async function handleDelete(id: number, name: string) {
    if (!isAdmin) return; // defence-in-depth: backend enforces admin too
    if (!window.confirm(`Delete enrolled face "${name}"? This cannot be undone.`)) return;
    manualCtrlRef.current?.abort();
    const ctrl = new AbortController();
    manualCtrlRef.current = ctrl;
    try {
      await api.deleteFace(id, ctrl.signal);
      if (ctrl.signal.aborted) return;
      setFaces((prev) => prev.filter((f) => f.id !== id));
    } catch (err) {
      if (ctrl.signal.aborted) return;
      setError(err instanceof Error ? err.message : "Delete failed");
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

      {/* Info banner */}
      <div className="bg-surface-raised border border-border rounded-lg p-4 text-sm text-muted">
        <p>
          Face recognition matches detected "person" events against enrolled face identities.
          Faces are enrolled via the API with pre-computed ArcFace embeddings.
          When the sentinel-infer face embedding endpoint is available, a photo upload
          workflow will be added here.
        </p>
      </div>

      {error && (
        <div className="bg-red-900/20 border border-red-800 rounded-lg p-4">
          <p className="text-red-400 text-sm">{error}</p>
        </div>
      )}

      {loading ? (
        <div className="flex-1 flex items-center justify-center text-muted">
          Loading faces...
        </div>
      ) : faces.length === 0 ? (
        <div className="flex-1 flex flex-col items-center justify-center gap-3 text-muted">
          <UserPlus className="w-12 h-12 opacity-20" />
          <p>No faces enrolled</p>
          <p className="text-xs text-faint max-w-md text-center">
            Use the API to enroll faces: POST /api/v1/faces with a JSON body
            containing "name" and "embedding" (float32 array).
          </p>
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
