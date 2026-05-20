import { HistoryController as SharedHistoryController, HistoryControllerProps } from "../lib/series";
import { MecomAPI } from "../api/mecom";

export function HistoryController({ onRangeChange, isLive, onSetLive }: HistoryControllerProps) {
  return (
    <SharedHistoryController
      onRangeChange={onRangeChange}
      isLive={isLive}
      onSetLive={onSetLive}
      fetchAvailability={() => MecomAPI.fetchAvailability()}
    />
  );
}
