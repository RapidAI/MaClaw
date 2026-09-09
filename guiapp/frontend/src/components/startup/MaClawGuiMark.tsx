import markUrl from '../../assets/images/maclaw-gui-mark.svg';

type MaClawGuiMarkProps = {
    title?: string;
    className?: string;
};

/** MaClaw GUI mark: cat assistant with headphones coding on a laptop. */
export function MaClawGuiMark({ title = 'MaClaw', className }: MaClawGuiMarkProps) {
    return <img src={markUrl} alt={title} className={className} width={192} height={220} draggable={false} decoding="sync" />;
}
