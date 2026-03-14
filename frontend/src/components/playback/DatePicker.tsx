/** DatePicker — custom mini-calendar for selecting recording dates (R6). */
import { useState, useRef, useEffect } from "react";
import { ChevronLeft, ChevronRight, Calendar } from "lucide-react";
import { getDaysInMonth, getFirstDayOfMonth } from "../../utils/time";

interface DatePickerProps {
  selectedDate: string;       // YYYY-MM-DD
  availableDays: Set<string>; // set of YYYY-MM-DD strings with recordings
  displayMonth: string;       // YYYY-MM (controls which month the calendar shows)
  onDateSelect: (date: string) => void;
  onMonthChange: (month: string) => void;
}

const DAY_NAMES = ["Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"];
const MONTH_NAMES = [
  "January", "February", "March", "April", "May", "June",
  "July", "August", "September", "October", "November", "December",
];

export default function DatePicker({
  selectedDate,
  availableDays,
  displayMonth,
  onDateSelect,
  onMonthChange,
}: DatePickerProps) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  // Close on outside click
  useEffect(() => {
    if (!open) return;
    const handleClick = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    document.addEventListener("mousedown", handleClick);
    return () => document.removeEventListener("mousedown", handleClick);
  }, [open]);

  // Parse displayMonth
  const [yearStr, monthStr] = displayMonth.split("-");
  const year = parseInt(yearStr, 10);
  const month = parseInt(monthStr, 10); // 1-indexed

  const daysInMonth = getDaysInMonth(year, month);
  const firstDay = getFirstDayOfMonth(year, month);

  const prevMonth = () => {
    const d = new Date(year, month - 2, 1);
    onMonthChange(`${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`);
  };

  const nextMonth = () => {
    const d = new Date(year, month, 1);
    onMonthChange(`${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`);
  };

  const today = new Date();
  const todayStr = `${today.getFullYear()}-${String(today.getMonth() + 1).padStart(2, "0")}-${String(today.getDate()).padStart(2, "0")}`;

  // Build calendar cells
  const cells: (number | null)[] = [];
  for (let i = 0; i < firstDay; i++) cells.push(null);
  for (let d = 1; d <= daysInMonth; d++) cells.push(d);
  while (cells.length % 7 !== 0) cells.push(null);

  // Format the selected date for the trigger button
  const displaySelected = selectedDate
    ? new Date(selectedDate + "T00:00:00").toLocaleDateString("en-US", {
        month: "short",
        day: "numeric",
        year: "numeric",
      })
    : "Select date...";

  return (
    <div ref={ref} className="relative">
      {/* Trigger button */}
      {/* TODO(review): L7 — add aria-expanded, aria-haspopup, Escape-to-close, keyboard navigation */}
      <button
        onClick={() => setOpen((o) => !o)}
        className="flex items-center gap-2 bg-surface-base border border-border rounded-lg
                   px-3 py-1.5 text-sm text-white hover:border-sentinel-500/50
                   focus:outline-none focus:ring-1 focus:ring-sentinel-500 transition-colors"
      >
        <Calendar className="w-4 h-4 text-muted" />
        {displaySelected}
      </button>

      {/* Dropdown calendar */}
      {open && (
        <div
          className="absolute top-full left-0 mt-1 z-50 bg-surface-raised border border-border
                     rounded-lg shadow-lg p-3 w-[280px]"
        >
          {/* Month header */}
          <div className="flex items-center justify-between mb-2">
            <button
              onClick={prevMonth}
              className="p-1 rounded hover:bg-surface-overlay text-muted hover:text-white transition-colors"
            >
              <ChevronLeft className="w-4 h-4" />
            </button>
            <span className="text-sm font-medium">
              {MONTH_NAMES[month - 1]} {year}
            </span>
            <button
              onClick={nextMonth}
              className="p-1 rounded hover:bg-surface-overlay text-muted hover:text-white transition-colors"
            >
              <ChevronRight className="w-4 h-4" />
            </button>
          </div>

          {/* Day-of-week headers */}
          <div className="grid grid-cols-7 mb-1">
            {DAY_NAMES.map((d) => (
              <div key={d} className="text-center text-xs text-faint py-1">
                {d}
              </div>
            ))}
          </div>

          {/* Calendar grid */}
          <div className="grid grid-cols-7">
            {cells.map((day, i) => {
              if (day === null) {
                return <div key={`empty-${i}`} className="h-8" />;
              }

              const dateStr = `${year}-${String(month).padStart(2, "0")}-${String(day).padStart(2, "0")}`;
              const isSelected = dateStr === selectedDate;
              const isToday = dateStr === todayStr;
              const hasRecordings = availableDays.has(dateStr);

              return (
                <button
                  key={dateStr}
                  onClick={() => {
                    onDateSelect(dateStr);
                    setOpen(false);
                  }}
                  className={`h-8 w-full rounded text-xs font-medium transition-colors relative
                    ${isSelected
                      ? "bg-sentinel-500 text-white"
                      : hasRecordings
                        ? "bg-sentinel-500/20 text-sentinel-400 hover:bg-sentinel-500/30"
                        : "text-muted hover:text-white hover:bg-surface-overlay"
                    }
                    ${isToday && !isSelected ? "ring-1 ring-sentinel-500/50" : ""}
                  `}
                >
                  {day}
                </button>
              );
            })}
          </div>
        </div>
      )}
    </div>
  );
}
