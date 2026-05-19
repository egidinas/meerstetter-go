import { HistoryController as SharedHistoryController } from "../lib/series";

export function HistoryController({ onRangeChange, isLive, onSetLive }) {
  return (
    <SharedHistoryController
      onRangeChange={onRangeChange}
      isLive={isLive}
      onSetLive={onSetLive}
      fetchAvailability={() => MecomAPI.fetchAvailability()}
    />
  );
}
