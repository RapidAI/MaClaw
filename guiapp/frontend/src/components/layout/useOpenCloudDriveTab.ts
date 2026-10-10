import { useEffect, type RefObject } from 'react';
import { EVENT_OPEN_CLOUD_DRIVE } from '../../constants/events';

/** Open the files tab when the shell asks for the cloud drive, unless it is already there. */
export function useOpenCloudDriveTab(
    navTabRef: RefObject<string>,
    setNavTabNow: (tab: string) => void,
): void {
    useEffect(() => {
        const openCloudDrive = () => {
            if (navTabRef.current === 'files') return;
            setNavTabNow('files');
        };
        window.addEventListener(EVENT_OPEN_CLOUD_DRIVE, openCloudDrive);
        return () => window.removeEventListener(EVENT_OPEN_CLOUD_DRIVE, openCloudDrive);
    }, [navTabRef, setNavTabNow]);
}
