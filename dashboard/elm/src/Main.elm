port module Main exposing (..)

import Browser
import Dict exposing (Dict)
import Html exposing (..)
import Html.Attributes exposing (..)
import Html.Events exposing (..)
import Json.Decode as D
import Process
import Task



-- MAIN


main : Program () Model Msg
main =
    Browser.element
        { init = init
        , update = update
        , subscriptions = subscriptions
        , view = view
        }



-- PORTS


port playUrl : String -> Cmd msg


port messageReceiver : (String -> msg) -> Sub msg



-- MODEL


type alias WebsocketMessage =
    { client_id : String
    , audio_url : String
    , text : String
    , username : String
    , user_picture : String
    }


type alias Card =
    { username : String
    , text : String
    , user_picture : String
    }


type alias Model =
    { cards : Dict Int Card
    , card_id : Int
    }


init : () -> ( Model, Cmd Msg )
init _ =
    ( Model Dict.empty 0, Cmd.none )



-- UPDATE


type Msg
    = Recv String
    | Remove Int


update : Msg -> Model -> ( Model, Cmd Msg )
update msg model =
    case msg of
        Remove id ->
            let
                new_dict =
                    Dict.remove id model.cards
            in
            ( { model | cards = new_dict }, Cmd.none )

        Recv json ->
            case D.decodeString websocketMessageDecoder json of
                Ok ws ->
                    let
                        next_id =
                            model.card_id + 1

                        new_dict =
                            Dict.insert next_id (Card ws.username ws.text ws.user_picture) model.cards
                    in
                    ( { model | card_id = next_id, cards = new_dict }
                    , Cmd.batch
                        [ playUrl ws.audio_url
                        , Process.sleep 15000 |> Task.perform (always (Remove next_id))
                        ]
                    )

                Err _ ->
                    ( model, Cmd.none )



-- SUBSCRIPTIONS


subscriptions : Model -> Sub Msg
subscriptions _ =
    messageReceiver Recv



-- VIEW


cardView : Card -> Html Msg
cardView card =
    div [ class "content" ]
        [ div [ class "user-picture" ] [ img [ src card.user_picture ] [] ]
        , div [ class "container" ]
            [ div [ class "username" ] [ text card.username ]
            , div [ class "text" ] [ text card.text ]
            ]
        ]


view : Model -> Html Msg
view model =
    let
        card_list =
            Dict.values model.cards
    in
    div [ class "main" ] (List.map cardView card_list)



-- JSON decode


websocketMessageDecoder : D.Decoder WebsocketMessage
websocketMessageDecoder =
    D.map5 WebsocketMessage
        (D.field "client_id" D.string)
        (D.field "audio_url" D.string)
        (D.field "text" D.string)
        (D.field "username" D.string)
        (D.field "user_picture" D.string)
